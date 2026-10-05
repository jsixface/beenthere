package ingest

import "testing"

func TestParseGeoJSONLocations(t *testing.T) {
	body := []byte(`{"locations":[{"type":"Feature","geometry":{"type":"Point","coordinates":[13.4,52.5]},
	 "properties":{"timestamp":"2023-11-14T22:13:20Z","altitude":34,"speed":1.5,"battery_level":0.5,"battery_state":"charging","device_id":"ios","motion":["walking"]}},
	 {"geometry":{"coordinates":[0,0]},"properties":{"timestamp":"1700000000"}}]}`)
	pts, err := ParseGeoJSONLocations(body)
	if err != nil || len(pts) != 1 {
		t.Fatalf("%v %v", pts, err)
	}
	p := pts[0]
	if p.Timestamp != 1700000000 || *p.Battery != 50 || *p.BatteryStatus != 2 || *p.TrackerID != "ios" || *p.Velocity != "1.5" {
		t.Fatalf("%+v", p)
	}
}

func TestBadTimestamp(t *testing.T) {
	_, err := ParseGeoJSONLocations([]byte(`{"locations":[{"geometry":{"coordinates":[1,2]},"properties":{"timestamp":"bogus"}}]}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOwnTracks(t *testing.T) {
	p, ok := OwnTracksPoint(map[string]any{"_type": "location", "lat": 52.5, "lon": 13.4, "tst": 1700000000.0,
		"tid": "AB", "vel": 36.0, "topic": "owntracks/u/d", "t": "u", "conn": "w", "bs": 2.0})
	if !ok || *p.Velocity != "10" || *p.Trigger != 5 || *p.Connection != 1 || *p.BatteryStatus != 2 {
		t.Fatalf("%+v %v", p, ok)
	}
	if _, ok := OwnTracksPoint(map[string]any{"_type": "waypoint"}); ok {
		t.Fatal("waypoint should be ignored")
	}
}

func TestTraccar(t *testing.T) {
	p, ok := TraccarPoint(map[string]any{"device_id": "x", "lat": "52.5", "lon": "13.4", "timestamp": "1700000000"})
	if !ok || p.Lat != 52.5 || *p.TrackerID != "x" {
		t.Fatalf("%+v", p)
	}
}
