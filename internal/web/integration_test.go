//go:build integration

package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/sixface/beenthere/internal/store"
)

func TestLoginFlow(t *testing.T) {
	dsn := os.Getenv("BT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("BT_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := store.Open(ctx, dsn, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct horse"), 4)
	_, _ = s.Pool.Exec(ctx, `DELETE FROM users WHERE email='web@example.com'`)
	if _, err := s.Pool.Exec(ctx, `INSERT INTO users (email, api_key, encrypted_password, status, created_at, updated_at) VALUES ('web@example.com','k',$1,1,now(),now())`, string(hash)); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(s, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), "secret").Routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	if r, _ := c.Get(srv.URL + "/map"); r.StatusCode != 303 || r.Header.Get("Location") != "/login" {
		t.Fatalf("unauthenticated /map: %d", r.StatusCode)
	}
	if r, _ := c.PostForm(srv.URL+"/login", url.Values{"email": {"web@example.com"}, "password": {"nope"}}); r.StatusCode != 401 {
		t.Fatalf("bad password: %d", r.StatusCode)
	}
	if r, _ := c.PostForm(srv.URL+"/login", url.Values{"email": {"missing@example.com"}, "password": {"x"}}); r.StatusCode != 401 {
		t.Fatalf("unknown user: %d", r.StatusCode)
	}
	r, _ := c.PostForm(srv.URL+"/login", url.Values{"email": {"WEB@example.com"}, "password": {"correct horse"}})
	if r.StatusCode != 303 {
		t.Fatalf("login: %d", r.StatusCode)
	}
	for _, p := range []string{"/map", "/stats", "/visits", "/trips", "/places", "/imports", "/exports", "/settings"} {
		r, _ := c.Get(srv.URL + p)
		b, _ := io.ReadAll(r.Body)
		if r.StatusCode != 200 || !strings.Contains(string(b), `"k"`) {
			t.Fatalf("%s: %d", p, r.StatusCode)
		}
	}
	if r, _ := c.Get(srv.URL + "/static/maplibre/maplibre-gl.mjs"); r.StatusCode != 200 || !strings.Contains(r.Header.Get("Content-Type"), "javascript") {
		t.Fatalf("static: %d %s", r.StatusCode, r.Header.Get("Content-Type"))
	}
	// changing the password invalidates existing sessions
	_, _ = s.Pool.Exec(ctx, `UPDATE users SET encrypted_password='changed' WHERE email='web@example.com'`)
	if r, _ := c.Get(srv.URL + "/map"); r.StatusCode != 303 {
		t.Fatalf("session should be invalid after password change: %d", r.StatusCode)
	}
}
