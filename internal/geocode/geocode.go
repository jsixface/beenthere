// Package geocode reverse-geocodes points through a configured Photon or
// Nominatim server. Nothing is contacted unless a host is configured, matching
// the Rails app's behaviour of being disabled without provider settings.
package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sixface/beenthere/internal/store"
)

type Provider int

const (
	Photon Provider = iota + 1
	Nominatim
)

type Config struct {
	Provider Provider
	BaseURL  string
	RPS      float64
}

// FromEnv reads PHOTON_API_HOST / NOMINATIM_API_HOST (+ *_USE_HTTPS) like Dawarich.
// It returns ok=false when geocoding is not configured.
func FromEnv() (Config, bool) {
	https := func(name string, def bool) string {
		v := strings.ToLower(os.Getenv(name))
		if v == "" {
			if def {
				return "https"
			}
			return "http"
		}
		if v == "true" || v == "1" {
			return "https"
		}
		return "http"
	}
	rps := 1.0
	if v, err := strconv.ParseFloat(os.Getenv("REVERSE_GEOCODING_RPS"), 64); err == nil && v > 0 {
		rps = v
	}
	if h := strings.TrimSpace(os.Getenv("PHOTON_API_HOST")); h != "" {
		return Config{Photon, https("PHOTON_API_USE_HTTPS", true) + "://" + h, rps}, true
	}
	if h := strings.TrimSpace(os.Getenv("NOMINATIM_API_HOST")); h != "" {
		return Config{Nominatim, https("NOMINATIM_API_USE_HTTPS", true) + "://" + h, rps}, true
	}
	return Config{}, false
}

type Result struct {
	City, Country string
	Raw           json.RawMessage
}

type Client struct {
	Cfg  Config
	HTTP *http.Client
}

func (c *Client) Reverse(ctx context.Context, lat, lon float64) (*Result, error) {
	var u string
	switch c.Cfg.Provider {
	case Photon:
		u = fmt.Sprintf("%s/reverse?lon=%v&lat=%v", c.Cfg.BaseURL, lon, lat)
	case Nominatim:
		u = fmt.Sprintf("%s/reverse?format=jsonv2&addressdetails=1&lat=%v&lon=%v", c.Cfg.BaseURL, lat, lon)
	default:
		return nil, fmt.Errorf("geocoding not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Beenthere/1.0 (self-hosted location history)")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, fmt.Errorf("geocoder returned %d", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		return &Result{}, nil // permanent: nothing known here
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return Parse(c.Cfg.Provider, body)
}

// Parse extracts city and country from a provider response.
func Parse(p Provider, body []byte) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	r := &Result{Raw: body}
	switch p {
	case Photon:
		feats, _ := m["features"].([]any)
		if len(feats) == 0 {
			return r, nil
		}
		f, _ := feats[0].(map[string]any)
		props, _ := f["properties"].(map[string]any)
		r.City = firstString(props, "city", "town", "village", "county")
		r.Country = firstString(props, "country")
		if b, err := json.Marshal(f); err == nil {
			r.Raw = b
		}
	case Nominatim:
		addr, _ := m["address"].(map[string]any)
		r.City = firstString(addr, "city", "town", "village", "hamlet", "municipality")
		r.Country = firstString(addr, "country")
	}
	return r, nil
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// Run geocodes un-geocoded points in the background until ctx is cancelled.
func Run(ctx context.Context, s *store.Store, cfg Config, log *slog.Logger) {
	cl := &Client{Cfg: cfg, HTTP: &http.Client{Timeout: 15 * time.Second}}
	interval := time.Duration(float64(time.Second) / cfg.RPS)
	log.Info("reverse geocoding enabled", "provider", cfg.BaseURL, "rps", cfg.RPS)
	for {
		n, err := batch(ctx, s, cl, interval)
		if ctx.Err() != nil {
			return
		}
		wait := 30 * time.Second
		if err != nil {
			log.Warn("reverse geocoding batch", "err", err)
			wait = time.Minute
		} else if n > 0 {
			wait = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func batch(ctx context.Context, s *store.Store, cl *Client, interval time.Duration) (int, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, ST_Y(lonlat::geometry), ST_X(lonlat::geometry) FROM points
		WHERE reverse_geocoded_at IS NULL AND lonlat IS NOT NULL ORDER BY id LIMIT 100`)
	if err != nil {
		return 0, err
	}
	type pt struct {
		id       int64
		lat, lon float64
	}
	var pts []pt
	for rows.Next() {
		var p pt
		if err := rows.Scan(&p.id, &p.lat, &p.lon); err != nil {
			rows.Close()
			return 0, err
		}
		pts = append(pts, p)
	}
	rows.Close()
	cache := map[[2]int64]*Result{}
	done := 0
	for _, p := range pts {
		key := [2]int64{int64(p.lat * 1e4), int64(p.lon * 1e4)} // ~11 m cells
		res, ok := cache[key]
		if !ok {
			select {
			case <-ctx.Done():
				return done, ctx.Err()
			case <-time.After(interval):
			}
			res, err = cl.Reverse(ctx, p.lat, p.lon)
			if err != nil {
				return done, err
			}
			cache[key] = res
		}
		var city, country any
		if res.City != "" {
			city = res.City
		}
		if res.Country != "" {
			country = res.Country
		}
		raw := res.Raw
		if len(raw) == 0 {
			raw = []byte("{}")
		}
		if _, err := s.Pool.Exec(ctx, `UPDATE points SET city=$2, country=coalesce($3, country),
			country_name=coalesce(country_name, $3), geodata=$4::jsonb, reverse_geocoded_at=now(), updated_at=now() WHERE id=$1`,
			p.id, city, country, string(raw)); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}
