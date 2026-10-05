package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sixface/beenthere/internal/store"
)

func TestTemplatesRender(t *testing.T) {
	w := New(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), "secret")
	u := &store.User{ID: 1, Email: `a"<b>@x.test`, APIKey: "key</script>", Settings: map[string]any{}}
	for name := range w.pages {
		rec := httptest.NewRecorder()
		v := view{Title: name, Active: name}
		if name != "login" {
			v.User = u
		}
		w.render(rec, name, v)
		body := rec.Body.String()
		if rec.Code != 200 || !strings.Contains(body, "Beenthere") {
			t.Errorf("%s: code %d", name, rec.Code)
		}
		if strings.Contains(body, "key</script>") {
			t.Errorf("%s: api key not escaped in script context", name)
		}
	}
	if len(w.pages) < 9 {
		t.Fatalf("expected all pages, got %d", len(w.pages))
	}
}

func TestSessionSignature(t *testing.T) {
	w := New(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), "secret")
	c := w.sign("1|9999999999|abcd")
	if !strings.Contains(c, ".") {
		t.Fatal("bad cookie")
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: c + "x"})
	if w.currentUser(r) != nil {
		t.Fatal("tampered cookie accepted")
	}
}
