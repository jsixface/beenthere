package api

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sixface/beenthere/internal/httpx"
	"github.com/sixface/beenthere/internal/store"
)

const (
	tileExtent = 4096
	tileBuffer = 256
	tileMargin = float64(tileBuffer) / tileExtent
	worldWidth = 40075016.685578488
)

func (s *Server) tileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tiles/points/{z}/{x}/{y}", s.auth(false, s.pointTile))
	mux.HandleFunc("GET /api/v1/tiles/tracks/{z}/{x}/{y}", s.auth(false, s.trackTile))
}

func tileCoords(r *http.Request) (z, x, y int, ok bool) {
	var err1, err2, err3 error
	z, err1 = strconv.Atoi(r.PathValue("z"))
	x, err2 = strconv.Atoi(r.PathValue("x"))
	ys := strings.TrimSuffix(r.PathValue("y"), ".mvt")
	y, err3 = strconv.Atoi(ys)
	if err1 != nil || err2 != nil || err3 != nil || z < 0 || z > 22 {
		return 0, 0, 0, false
	}
	max := (1 << z) - 1
	return z, x, y, x >= 0 && y >= 0 && x <= max && y <= max
}

func (s *Server) writeTile(w http.ResponseWriter, r *http.Request, sql string, args ...any) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var tile []byte
	err := s.S.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = 15000"); err != nil {
			return err
		}
		return tx.QueryRow(ctx, sql, args...).Scan(&tile)
	})
	if err != nil {
		s.Log.Warn("tile query failed", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "tile unavailable")
		return
	}
	sum := md5.Sum(tile)
	etag := `"` + hex.EncodeToString(sum[:]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
	_, _ = w.Write(tile)
}

// $1 user, $2 start, $3 end, $4 import (nullable), $5 z, $6 x, $7 y
const pointTileSQL = `
WITH env AS (SELECT ST_TileEnvelope($5::int,$6::int,$7::int) AS e, ST_TileEnvelope($5::int,$6::int,$7::int, margin => %MARGIN%) AS m),
cand AS (
  SELECT p.id, p.timestamp, p.battery, p.track_id, p.lock_version, coalesce(p.altitude_decimal, p.altitude) AS altitude, p.velocity,
         ST_Y(p.lonlat::geometry) AS latitude, ST_X(p.lonlat::geometry) AS longitude,
         ST_Transform(p.lonlat::geometry, 3857) AS g
  FROM points p, env
  WHERE p.user_id=$1 AND p.anomaly IS NOT TRUE AND p.timestamp BETWEEN $2 AND $3
    AND ($4::bigint IS NULL OR p.import_id=$4)
    AND p.lonlat IS NOT NULL
    AND (($5::int < 2) OR p.lonlat && ST_Transform(env.m, 4326)::geography)
    AND ST_Intersects(p.lonlat::geometry, ST_Transform(env.m, 4326))
),
features AS (
  SELECT count(*) AS count, min(timestamp) AS timestamp, max(timestamp) AS max_timestamp,
    %ATTRS%
    ST_AsMVTGeom(ST_Centroid(ST_Collect(g)), (SELECT e FROM env), 4096, 256, true) AS geom
  FROM cand
  GROUP BY ST_SnapToGrid(g, (%WORLD% / (1 << $5::int)) / 512 * (CASE WHEN $5::int < 14 THEN 4 ELSE 1 END))
  LIMIT 300000
)
SELECT coalesce(ST_AsMVT(features.*, 'points', 4096, 'geom'), ''::bytea) FROM features WHERE geom IS NOT NULL`

func (s *Server) pointTile(w http.ResponseWriter, r *http.Request, u *store.User) {
	z, x, y, ok := tileCoords(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid tile coordinates")
		return
	}
	q := r.URL.Query()
	start, end := int64(0), time.Now().Unix()+86400
	if v := q.Get("start_at"); v != "" {
		start = safeTimestamp(v)
	}
	if v := q.Get("end_at"); v != "" {
		end = safeTimestamp(v)
	}
	var imp *int64
	if v, err := strconv.ParseInt(q.Get("import_id"), 10, 64); err == nil {
		imp = &v
	}
	attrs := ""
	if z >= 5 {
		attrs = `min(id) AS id, min(battery) AS battery, min(track_id) AS track_id, min(lock_version) AS revision,
		  min(altitude) AS altitude, min(velocity) AS velocity, min(latitude) AS latitude, min(longitude) AS longitude,`
	}
	sql := strings.NewReplacer("%MARGIN%", strconv.FormatFloat(tileMargin, 'f', -1, 64),
		"%WORLD%", strconv.FormatFloat(worldWidth, 'f', -1, 64), "%ATTRS%", attrs).Replace(pointTileSQL)
	s.writeTile(w, r, sql, u.ID, start, end, imp, z, x, y)
}

// $1 user, $2 from (timestamptz), $3 to, $4 import, $5 z, $6 x, $7 y
const trackTileSQL = `
WITH env AS (SELECT ST_TileEnvelope($5::int,$6::int,$7::int) AS e, ST_TileEnvelope($5::int,$6::int,$7::int, margin => %MARGIN%) AS m),
features AS (
  SELECT t.id, extract(epoch FROM t.start_at)::bigint AS start_at, extract(epoch FROM t.end_at)::bigint AS end_at,
    t.distance, t.avg_speed, t.duration, t.dominant_mode, t.lock_version AS revision,
    ST_AsMVTGeom(
      CASE WHEN $5::int >= 14 THEN ST_Transform(t.original_path, 3857)
           ELSE ST_Simplify(ST_Transform(t.original_path, 3857), (%WORLD% / (1 << $5::int)) / 512) END,
      (SELECT e FROM env), 4096, 256, true) AS geom
  FROM tracks t, env
  WHERE t.user_id=$1 AND t.end_at >= $2 AND t.start_at <= $3
    AND ($4::bigint IS NULL OR t.id IN (SELECT track_id FROM points WHERE import_id=$4 AND track_id IS NOT NULL))
    AND t.original_path && ST_Transform(env.m, 4326)
  LIMIT 20000
)
SELECT coalesce(ST_AsMVT(features.*, 'tracks', 4096, 'geom'), ''::bytea) FROM features WHERE geom IS NOT NULL`

func (s *Server) trackTile(w http.ResponseWriter, r *http.Request, u *store.User) {
	z, x, y, ok := tileCoords(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid tile coordinates")
		return
	}
	q := r.URL.Query()
	from, to := time.Unix(0, 0).UTC(), time.Now().Add(24*time.Hour).UTC()
	if v := q.Get("start_at"); v != "" {
		from = time.Unix(safeTimestamp(v), 0).UTC()
	}
	if v := q.Get("end_at"); v != "" {
		to = time.Unix(safeTimestamp(v), 0).UTC()
	}
	var imp *int64
	if v, err := strconv.ParseInt(q.Get("import_id"), 10, 64); err == nil {
		imp = &v
	}
	sql := strings.NewReplacer("%MARGIN%", strconv.FormatFloat(tileMargin, 'f', -1, 64),
		"%WORLD%", strconv.FormatFloat(worldWidth, 'f', -1, 64)).Replace(trackTileSQL)
	s.writeTile(w, r, sql, u.ID, from, to, imp, z, x, y)
}
