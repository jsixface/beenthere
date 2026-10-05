package geocode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParsePhoton(t *testing.T) {
	r, err := Parse(Photon, []byte(`{"features":[{"properties":{"city":"Berlin","country":"Germany"},"geometry":{}}]}`))
	if err != nil || r.City != "Berlin" || r.Country != "Germany" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestParseNominatim(t *testing.T) {
	r, _ := Parse(Nominatim, []byte(`{"address":{"town":"Bergen","country":"Norway"}}`))
	if r.City != "Bergen" || r.Country != "Norway" {
		t.Fatalf("%+v", r)
	}
}

func TestClientReverse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/reverse") || r.Header.Get("User-Agent") == "" {
			w.WriteHeader(400)
			return
		}
		w.Write([]byte(`{"features":[{"properties":{"city":"Paris","country":"France"}}]}`))
	}))
	defer srv.Close()
	c := &Client{Cfg: Config{Provider: Photon, BaseURL: srv.URL}, HTTP: srv.Client()}
	r, err := c.Reverse(context.Background(), 48.85, 2.35)
	if err != nil || r.City != "Paris" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestFromEnvDisabledByDefault(t *testing.T) {
	t.Setenv("PHOTON_API_HOST", "")
	t.Setenv("NOMINATIM_API_HOST", "")
	if _, ok := FromEnv(); ok {
		t.Fatal("should be disabled without host")
	}
	t.Setenv("PHOTON_API_HOST", "photon.local:2322")
	t.Setenv("PHOTON_API_USE_HTTPS", "false")
	c, ok := FromEnv()
	if !ok || c.BaseURL != "http://photon.local:2322" {
		t.Fatalf("%+v", c)
	}
}
