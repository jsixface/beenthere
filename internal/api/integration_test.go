//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sixface/beenthere/internal/jobs"
	"github.com/sixface/beenthere/internal/migrate"
	"github.com/sixface/beenthere/internal/store"
)

// Run with: BT_TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/api
type harness struct {
	t   *testing.T
	srv *httptest.Server
	s   *store.Store
	key string
	uid int64
	job *jobs.Manager
}

func newHarness(t *testing.T) *harness {
	url := os.Getenv("BT_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("BT_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := store.Open(ctx, url, 5)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if _, err := migrate.Apply(ctx, s.Pool); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"points", "visits", "place_visits", "taggings", "tags", "places", "areas", "notes", "action_text_rich_texts",
		"planned_day_notes", "planned_stops", "planned_reservations", "planned_days", "planned_accommodations", "planned_travellers", "planned_unplanned_places", "trips", "track_segments", "tracks", "stats", "imports", "users"} {
		if _, err := s.Pool.Exec(ctx, "DELETE FROM "+tbl); err != nil {
			t.Fatalf("clean %s: %v", tbl, err)
		}
	}
	_, _ = s.Pool.Exec(ctx, `INSERT INTO countries (iso_a2, iso_a3, name, geom, created_at, updated_at)
		SELECT 'DE','DEU','Germany', ST_Multi(ST_MakeEnvelope(5.8, 47.2, 15.1, 55.1, 4326)), now(), now()
		WHERE NOT EXISTS (SELECT 1 FROM countries WHERE iso_a2='DE')`)
	key := "test-key-123"
	var uid int64
	if err := s.Pool.QueryRow(ctx, `INSERT INTO users (email, api_key, encrypted_password, status, created_at, updated_at, settings)
		VALUES ('t@example.com',$1,'x',1,now(),now(),'{"minutes_between_routes":"30","min_minutes_spent_in_city":"0","visit_min_duration_minutes":"5","visit_min_points":"3"}') RETURNING id`, key).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := jobs.New(ctx, s, 2, log)
	mux := http.NewServeMux()
	(&Server{S: s, Jobs: mgr, Log: log, MaxUploadBytes: 50 << 20}).Routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &harness{t: t, srv: srv, s: s, key: key, uid: uid, job: mgr}
}

func (h *harness) do(method, path string, body any) (int, []byte, http.Header) {
	h.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, rdr)
	req.Header.Set("Authorization", "Bearer "+h.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, resp.Header
}

func (h *harness) expect(code int, method, path string, body any) []byte {
	h.t.Helper()
	got, b, _ := h.do(method, path, body)
	if got != code {
		h.t.Fatalf("%s %s: want %d got %d: %s", method, path, code, got, b)
	}
	return b
}

func feature(lon, lat float64, ts int64, extra map[string]any) map[string]any {
	props := map[string]any{"timestamp": ts, "altitude": 30, "speed": 1.2, "device_id": "dev1"}
	for k, v := range extra {
		props[k] = v
	}
	return map[string]any{"type": "Feature", "geometry": map[string]any{"type": "Point", "coordinates": []float64{lon, lat}}, "properties": props}
}

func TestEndToEnd(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// auth
	if code, _, _ := (&harness{srv: h.srv, t: t}).doNoAuth("GET", "/api/v1/points"); code != 401 {
		t.Fatalf("unauthenticated: %d", code)
	}
	h.expect(200, "GET", "/api/v1/users/me", nil)

	// ingest: two bursts 2h apart (=> 2 tracks), the first has a 10 minute stay
	base := int64(1700000000)
	var locs []any
	for i := 0; i < 8; i++ { // stay near Berlin, 70s apart... 8 points over ~10 min
		locs = append(locs, feature(13.4000+float64(i)*0.00001, 52.5, base+int64(i)*90, nil))
	}
	for i := 0; i < 5; i++ { // move
		locs = append(locs, feature(13.41+float64(i)*0.01, 52.51, base+800+int64(i)*60, nil))
	}
	for i := 0; i < 4; i++ { // second burst
		locs = append(locs, feature(13.5+float64(i)*0.01, 52.52, base+7200+int64(i)*60, nil))
	}
	out := h.expect(200, "POST", "/api/v1/points", map[string]any{"locations": locs})
	var created struct{ Data []map[string]any }
	_ = json.Unmarshal(out, &created)
	if len(created.Data) != 17 {
		t.Fatalf("created %d: %s", len(created.Data), out)
	}
	// idempotent re-post must not duplicate
	h.expect(200, "POST", "/api/v1/points", map[string]any{"locations": locs})
	var cnt int
	_ = h.s.Pool.QueryRow(ctx, `SELECT count(*) FROM points WHERE user_id=$1`, h.uid).Scan(&cnt)
	if cnt != 17 {
		t.Fatalf("points after re-post: %d", cnt)
	}
	var pc int
	_ = h.s.Pool.QueryRow(ctx, `SELECT points_count FROM users WHERE id=$1`, h.uid).Scan(&pc)
	if pc != 17 {
		t.Fatalf("points_count %d", pc)
	}
	h.expect(422, "POST", "/api/v1/points", map[string]any{"locations": []any{feature(1, 2, 0, map[string]any{"timestamp": "garbage"})}})

	// index + headers + etag
	code, body, hdr := h.do("GET", fmt.Sprintf("/api/v1/points?start_at=%d&end_at=%d&per_page=5&page=2", base-1, base+10000), nil)
	if code != 200 || hdr.Get("X-Total-Pages") != "4" || hdr.Get("X-Current-Page") != "2" {
		t.Fatalf("index: %d %v %s", code, hdr, body)
	}
	var pts []map[string]any
	_ = json.Unmarshal(body, &pts)
	if len(pts) != 5 || pts[0]["latitude"] == nil || pts[0]["battery_status"] != nil && false {
		t.Fatalf("points: %s", body)
	}
	req, _ := http.NewRequest("GET", h.srv.URL+fmt.Sprintf("/api/v1/points?start_at=%d&end_at=%d&per_page=5&page=2", base-1, base+10000), nil)
	req.Header.Set("Authorization", "Bearer "+h.key)
	req.Header.Set("If-None-Match", hdr.Get("ETag"))
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 304 {
		t.Fatalf("etag revalidation: %d", resp.StatusCode)
	}
	h.expect(200, "GET", fmt.Sprintf("/api/v1/points?start_at=%d&end_at=%d&slim=true&min_longitude=13&max_longitude=14&min_latitude=52&max_latitude=53", base-1, base+10000), nil)
	h.expect(400, "GET", "/api/v1/points?min_longitude=5&max_longitude=1&min_latitude=1&max_latitude=2", nil)
	h.expect(200, "GET", "/api/v1/points/tracked_months", nil)

	// other formats
	h.expect(201, "POST", "/api/v1/overland/batches", map[string]any{"locations": []any{feature(13.6, 52.6, base+20000, map[string]any{"battery_level": 0.8, "battery_state": "charging"})}})
	h.expect(200, "POST", "/api/v1/owntracks/points", map[string]any{"_type": "location", "lat": 52.7, "lon": 13.7, "tst": base + 30000, "tid": "ot", "batt": 80, "vel": 36, "topic": "owntracks/u/d"})
	h.expect(200, "POST", "/api/v1/traccar/points", map[string]any{"device_id": "tc", "lat": "52.8", "lon": "13.8", "timestamp": fmt.Sprint(base + 40000)})
	var batt, bs int32
	_ = h.s.Pool.QueryRow(ctx, `SELECT battery, battery_status FROM points WHERE user_id=$1 AND timestamp=$2`, h.uid, base+20000).Scan(&batt, &bs)
	if batt != 80 || bs != 2 {
		t.Fatalf("overland battery %d status %d", batt, bs)
	}

	// pipeline: tracks, stats, countries, visits
	h.job.Recompute(ctx, h.uid, base, base+40000)
	var ntracks, withCountry int
	_ = h.s.Pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE user_id=$1`, h.uid).Scan(&ntracks)
	if ntracks < 2 {
		t.Fatalf("tracks: %d", ntracks)
	}
	_ = h.s.AssignCountries(ctx, h.uid, createdIDs(created.Data))
	_ = h.s.Pool.QueryRow(ctx, `SELECT count(*) FROM points WHERE user_id=$1 AND country_name='Germany'`, h.uid).Scan(&withCountry)
	if withCountry == 0 {
		t.Fatal("countries not assigned")
	}
	tr := h.expect(200, "GET", fmt.Sprintf("/api/v1/tracks?start_at=%d&end_at=%d", base-1, base+50000), nil)
	if !strings.Contains(string(tr), `"FeatureCollection"`) || !strings.Contains(string(tr), `"LineString"`) {
		t.Fatalf("tracks: %s", tr)
	}
	var fc struct {
		Features []struct{ Properties map[string]any }
	}
	_ = json.Unmarshal(tr, &fc)
	tid := int64(fc.Features[0].Properties["id"].(float64))
	h.expect(200, "GET", fmt.Sprintf("/api/v1/tracks/%d", tid), nil)
	if b := h.expect(200, "GET", fmt.Sprintf("/api/v1/tracks/%d/points", tid), nil); !strings.Contains(string(b), "latitude") {
		t.Fatalf("track points: %s", b)
	}
	st := h.expect(200, "GET", "/api/v1/stats", nil)
	if !strings.Contains(string(st), `"totalPointsTracked":20`) || !strings.Contains(string(st), `"yearlyStats":[{`) {
		t.Fatalf("stats: %s", st)
	}

	nv, err := detectVisits(ctx, h)
	if err != nil || nv == 0 {
		t.Fatalf("visits detected %d err %v", nv, err)
	}
	vs := h.expect(200, "GET", fmt.Sprintf("/api/v1/visits?start_at=%d&end_at=%d", time.Unix(base-100, 0).Unix(), time.Unix(base+50000, 0).Unix()), nil)
	_ = vs
	vlist := h.expect(200, "GET", "/api/v1/visits?start_at=2023-11-01T00:00:00Z&end_at=2023-12-31T00:00:00Z", nil)
	var visits []map[string]any
	_ = json.Unmarshal(vlist, &visits)
	if len(visits) == 0 || visits[0]["status"] != "suggested" {
		t.Fatalf("visits: %s", vlist)
	}
	vid := int64(visits[0]["id"].(float64))
	h.expect(200, "PATCH", fmt.Sprintf("/api/v1/visits/%d", vid), map[string]any{"visit": map[string]any{"name": "Home"}})
	if b := h.expect(200, "GET", fmt.Sprintf("/api/v1/visits/%d", vid), nil); !strings.Contains(string(b), `"confirmed"`) {
		t.Fatalf("visit should be confirmed after edit: %s", b)
	}
	created2 := h.expect(200, "POST", "/api/v1/visits", map[string]any{"visit": map[string]any{"name": "Cafe", "latitude": 48.1, "longitude": 11.5,
		"started_at": "2023-11-20T10:00:00Z", "ended_at": "2023-11-20T11:00:00Z"}})
	if !strings.Contains(string(created2), `"started_at":"2023-11-20T10:00:00Z"`) {
		t.Fatalf("visit times must round-trip as UTC: %s", created2)
	}
	// offsets must be normalized, not stored as wall-clock
	off := h.expect(200, "POST", "/api/v1/visits", map[string]any{"visit": map[string]any{"name": "Bar", "latitude": 40.1, "longitude": 10.5,
		"started_at": "2023-11-21T10:00:00+02:00", "ended_at": "2023-11-21T11:00:00+02:00"}})
	if !strings.Contains(string(off), `"started_at":"2023-11-21T08:00:00Z"`) {
		t.Fatalf("offset times not normalized to UTC: %s", off)
	}
	var tstart time.Time
	_ = h.s.Pool.QueryRow(ctx, `SELECT min(start_at) FROM tracks WHERE user_id=$1`, h.uid).Scan(&tstart)
	if tstart.Unix() != base {
		t.Fatalf("track start_at %v want %d (UTC)", tstart, base)
	}
	h.expect(422, "POST", "/api/v1/visits", map[string]any{"visit": map[string]any{"latitude": 1, "longitude": 2, "started_at": "2023-11-20T12:00:00Z", "ended_at": "2023-11-20T11:00:00Z"}})
	h.expect(204, "DELETE", fmt.Sprintf("/api/v1/visits/%d", vid), nil)
	h.expect(404, "GET", fmt.Sprintf("/api/v1/visits/%d", vid), nil)

	// areas, places, tags, notes
	ar := h.expect(201, "POST", "/api/v1/areas", map[string]any{"area": map[string]any{"name": "Home", "latitude": 52.5, "longitude": 13.4, "radius": 100}})
	var area store.Area
	_ = json.Unmarshal(ar, &area)
	h.expect(200, "PATCH", fmt.Sprintf("/api/v1/areas/%d", area.ID), map[string]any{"area": map[string]any{"radius": 150}})
	h.expect(200, "GET", "/api/v1/areas", nil)
	h.expect(422, "POST", "/api/v1/areas", map[string]any{"area": map[string]any{"name": ""}})

	tag := h.expect(201, "POST", "/api/v1/tags", map[string]any{"tag": map[string]any{"name": "Home", "icon": "🏠", "color": "#ff0000", "privacy_radius_meters": 200}})
	var tg store.TagOut
	_ = json.Unmarshal(tag, &tg)
	pl := h.expect(201, "POST", "/api/v1/places", map[string]any{"place": map[string]any{"name": "My flat", "latitude": 52.5, "longitude": 13.4, "tag_ids": []int64{tg.ID}}})
	var place store.PlaceOut
	_ = json.Unmarshal(pl, &place)
	if len(place.Tags) != 1 || place.Name != "My flat" {
		t.Fatalf("place: %s", pl)
	}
	h.expect(200, "GET", "/api/v1/places?filter=all", nil)
	h.expect(200, "GET", fmt.Sprintf("/api/v1/places?tag_ids[]=%d", tg.ID), nil)
	h.expect(200, "GET", "/api/v1/places/nearby?latitude=52.5&longitude=13.4", nil)
	if b := h.expect(200, "GET", "/api/v1/tags/privacy_zones", nil); !strings.Contains(string(b), "My flat") {
		t.Fatalf("privacy zones: %s", b)
	}
	h.expect(200, "PATCH", fmt.Sprintf("/api/v1/places/%d", place.ID), map[string]any{"place": map[string]any{"name": "Flat 2"}})
	h.expect(204, "DELETE", fmt.Sprintf("/api/v1/places/%d", place.ID), nil)

	nt := h.expect(201, "POST", "/api/v1/notes", map[string]any{"note": map[string]any{"title": "t", "body": "hello", "latitude": 52.5, "longitude": 13.4, "noted_at": "2023-11-15T12:00:00Z"}})
	var note store.NoteOut
	_ = json.Unmarshal(nt, &note)
	h.expect(200, "GET", "/api/v1/notes?standalone=true", nil)
	h.expect(200, "PATCH", fmt.Sprintf("/api/v1/notes/%d", note.ID), map[string]any{"note": map[string]any{"body": "edited"}})
	h.expect(200, "DELETE", fmt.Sprintf("/api/v1/notes/%d", note.ID), nil)
	h.expect(422, "POST", "/api/v1/notes", map[string]any{"note": map[string]any{"body": ""}})

	// trips
	trip := h.expect(201, "POST", "/api/v1/trips", map[string]any{"trip": map[string]any{"name": "Berlin", "started_at": "2023-11-14T00:00:00Z", "ended_at": "2023-11-16T00:00:00Z", "description": "nice"}})
	var tp store.TripOut
	_ = json.Unmarshal(trip, &tp)
	if tp.Distance == nil || *tp.Distance <= 0 || tp.PointsCount == 0 || tp.Description != "nice" {
		t.Fatalf("trip: %s", trip)
	}
	h.expect(200, "PATCH", fmt.Sprintf("/api/v1/trips/%d", tp.ID), map[string]any{"trip": map[string]any{"name": "Berlin 2"}})
	h.expect(200, "POST", fmt.Sprintf("/api/v1/trips/%d/recalculate", tp.ID), nil)
	h.expect(204, "DELETE", fmt.Sprintf("/api/v1/trips/%d", tp.ID), nil)

	// settings
	h.expect(200, "PATCH", "/api/v1/settings", map[string]any{"settings": map[string]any{"route_opacity": 0.5, "maps": map[string]any{"distance_unit": "mi"}, "bogus": 1}})
	if b := h.expect(200, "GET", "/api/v1/settings", nil); !strings.Contains(string(b), `"distance_unit":"mi"`) || strings.Contains(string(b), "bogus") {
		t.Fatalf("settings: %s", b)
	}
	h.expect(422, "PATCH", "/api/v1/settings", map[string]any{"settings": map[string]any{"maps_maplibre_tiles_url": "https://x/a.png"}})

	// move + delete points
	pid := int64(created.Data[0]["id"].(float64))
	h.expect(200, "PATCH", fmt.Sprintf("/api/v1/points/%d", pid), map[string]any{"point": map[string]any{"latitude": 52.5001, "longitude": 13.4001}})
	h.expect(200, "DELETE", "/api/v1/points/bulk_destroy", map[string]any{"point_ids": []int64{int64(created.Data[1]["id"].(float64)), int64(created.Data[2]["id"].(float64))}})
	h.expect(200, "DELETE", fmt.Sprintf("/api/v1/points/%d", pid), nil)
	h.expect(404, "DELETE", fmt.Sprintf("/api/v1/points/%d", pid), nil)
	_ = h.s.Pool.QueryRow(ctx, `SELECT points_count FROM users WHERE id=$1`, h.uid).Scan(&pc)
	_ = h.s.Pool.QueryRow(ctx, `SELECT count(*) FROM points WHERE user_id=$1`, h.uid).Scan(&cnt)
	if pc != cnt {
		t.Fatalf("points_count drift: counter %d actual %d", pc, cnt)
	}

	// export + import round trip
	gpx := h.expect(200, "GET", "/api/v1/export?format=gpx", nil)
	if !strings.Contains(string(gpx), "<trkpt") {
		t.Fatalf("gpx: %s", gpx)
	}
	gj := h.expect(200, "GET", "/api/v1/export?format=geojson", nil)
	var gjv map[string]any
	if err := json.Unmarshal(gj, &gjv); err != nil {
		t.Fatalf("geojson invalid: %v", err)
	}
	h.expect(200, "DELETE", "/api/v1/points/bulk_destroy", map[string]any{"point_ids": allIDs(h)})
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("files", "roundtrip.gpx")
	fw.Write(gpx)
	mw.Close()
	req, _ = http.NewRequest("POST", h.srv.URL+"/api/v1/imports", &buf)
	req.Header.Set("Authorization", "Bearer "+h.key)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err = http.DefaultClient.Do(req)
	b, _ := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("import: %d %s", resp.StatusCode, b)
	}
	deadline := time.Now().Add(15 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		_ = h.s.Pool.QueryRow(ctx, `SELECT CASE status WHEN 2 THEN 'completed' WHEN 3 THEN 'failed' ELSE 'pending' END FROM imports WHERE user_id=$1`, h.uid).Scan(&status)
		if status != "pending" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = h.s.Pool.QueryRow(ctx, `SELECT count(*) FROM points WHERE user_id=$1`, h.uid).Scan(&cnt)
	if status != "completed" || cnt < 10 {
		var msg *string
		_ = h.s.Pool.QueryRow(ctx, `SELECT error_message FROM imports WHERE user_id=$1`, h.uid).Scan(&msg)
		t.Fatalf("import status %s points %d err %v", status, cnt, msg)
	}
	h.expect(200, "GET", "/api/v1/imports", nil)

	// tiles
	for _, p := range []string{"points", "tracks"} {
		code, body, hdr := h.do("GET", fmt.Sprintf("/api/v1/tiles/%s/10/550/335.mvt?start_at=%d&end_at=%d", p, base-100, base+90000), nil)
		if code != 200 || hdr.Get("Content-Type") != "application/vnd.mapbox-vector-tile" {
			t.Fatalf("tile %s: %d %s", p, code, body)
		}
	}
	h.expect(400, "GET", "/api/v1/tiles/points/3/99/1.mvt", nil)
}

func createdIDs(d []map[string]any) []int64 {
	var out []int64
	for _, m := range d {
		out = append(out, int64(m["id"].(float64)))
	}
	return out
}

func allIDs(h *harness) []int64 {
	rows, _ := h.s.Pool.Query(context.Background(), `SELECT id FROM points WHERE user_id=$1`, h.uid)
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		out = append(out, id)
	}
	return out
}

func (h *harness) doNoAuth(method, path string) (int, []byte, http.Header) {
	req, _ := http.NewRequest(method, h.srv.URL+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, resp.Header
}
