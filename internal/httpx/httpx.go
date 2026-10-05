// Package httpx contains small HTTP helpers shared by the API and web layers.
package httpx

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

const Version = "1.0.0-go"

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

func Errors(w http.ResponseWriter, status int, msgs ...string) {
	JSON(w, status, map[string]any{"errors": msgs})
}

// APIKey extracts the key from ?api_key= or an Authorization: Bearer header.
func APIKey(r *http.Request) string {
	if k := r.URL.Query().Get("api_key"); k != "" {
		return k
	}
	if h := r.Header.Get("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// DecodeBody reads a JSON object body into a generic map; form bodies are also accepted.
func DecodeBody(r *http.Request) (map[string]any, error) {
	out := map[string]any{}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") || r.Header.Get("Content-Type") == "" {
		if r.Body == nil {
			return out, nil
		}
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&out); err != nil && err.Error() != "EOF" {
			return nil, err
		}
		return out, nil
	}
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	for k, v := range r.Form {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out, nil
}

func QueryInt(r *http.Request, key string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil {
		return v
	}
	return def
}

func QueryBool(r *http.Request, key string) bool {
	v := strings.ToLower(r.URL.Query().Get(key))
	return v == "true" || v == "1" || v == "t"
}

func PathID(r *http.Request, name string) (int64, bool) {
	n, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n, err == nil
}

// Float coerces a decoded JSON value to float64.
func Float(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	}
	return 0, false
}

func String(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}
