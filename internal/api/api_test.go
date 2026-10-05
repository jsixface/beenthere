package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoutesRegisterWithoutConflict(t *testing.T) {
	mux := http.NewServeMux()
	(&Server{}).Routes(mux) // panics on ambiguous patterns
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/health", nil))
	if rec.Code != 200 || rec.Header().Get("X-Dawarich-Version") == "" {
		t.Fatalf("health: %d", rec.Code)
	}
}

func TestSafeTimestamp(t *testing.T) {
	if safeTimestamp("1700000000") != 1700000000 {
		t.Fatal("numeric")
	}
	if safeTimestamp("2023-11-14T22:13:20Z") != 1700000000 {
		t.Fatal("rfc3339")
	}
}

func TestBBox(t *testing.T) {
	r := httptest.NewRequest("GET", "/?min_longitude=1&max_longitude=2&min_latitude=3&max_latitude=4", nil)
	b, present, ok := parseBBox(r)
	if !present || !ok || b[2] != 2 {
		t.Fatal("valid bbox")
	}
	r = httptest.NewRequest("GET", "/?min_longitude=5&max_longitude=2&min_latitude=3&max_latitude=4", nil)
	if _, _, ok := parseBBox(r); ok {
		t.Fatal("inverted bbox should be invalid")
	}
}

func TestValidTilesURL(t *testing.T) {
	for in, want := range map[string]bool{
		"https://x/{z}/{x}/{y}.png": true, "https://x/style.json": true, "/styles/a.json": true,
		"https://x/a.png": false, "javascript:alert(1)": false,
	} {
		if validTilesURL(in) != want {
			t.Errorf("%s", in)
		}
	}
}
