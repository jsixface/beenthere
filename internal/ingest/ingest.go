// Package ingest converts the payload formats sent by tracking apps
// (Dawarich native/Overland GeoJSON, OwnTracks, Traccar) into store.PointIn.
package ingest

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/sixface/beenthere/internal/geo"
	"github.com/sixface/beenthere/internal/store"
)

func str(v any) *string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		if t == "" {
			return nil
		}
		return &t
	case float64:
		s := strconv.FormatFloat(t, 'f', -1, 64)
		return &s
	case json.Number:
		s := t.String()
		return &s
	}
	return nil
}

func num(v any) *float64 {
	switch t := v.(type) {
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return nil
		}
		return &t
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		return &f
	}
	return nil
}

func i32(v any) *int32 {
	f := num(v)
	if f == nil {
		return nil
	}
	n := int32(*f)
	return &n
}

func strList(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func motion(props map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"motion", "activity", "action", "departure_date"} {
		if v, ok := props[k]; ok && v != nil && v != false {
			out[k] = v
		}
	}
	return out
}

func batteryLevel(v any) *int32 {
	f := num(v)
	if f == nil {
		return nil
	}
	n := int32(*f * 100)
	if n <= 0 {
		return nil
	}
	return &n
}

// GeoJSONBatch is the payload of POST /api/v1/points and Overland batches.
type GeoJSONBatch struct {
	Locations []map[string]any `json:"locations"`
}

// ParseGeoJSONLocations handles both the native Dawarich payload
// ({"locations":[...]}) and bare arrays. It returns ErrInvalid for malformed
// timestamps so the API can answer 422.
func ParseGeoJSONLocations(body []byte) ([]store.PointIn, error) {
	var locs []map[string]any
	var batch GeoJSONBatch
	if err := json.Unmarshal(body, &batch); err == nil && batch.Locations != nil {
		locs = batch.Locations
	} else {
		var arr []map[string]any
		if err := json.Unmarshal(body, &arr); err != nil {
			var obj map[string]any
			if err2 := json.Unmarshal(body, &obj); err2 != nil {
				return nil, fmt.Errorf("invalid JSON: %w", err2)
			}
			if _, ok := obj["geometry"]; ok { // single Feature
				arr = []map[string]any{obj}
			}
		}
		locs = arr
	}
	out := make([]store.PointIn, 0, len(locs))
	for _, loc := range locs {
		p, ok, err := geoJSONPoint(loc)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func geoJSONPoint(loc map[string]any) (store.PointIn, bool, error) {
	var p store.PointIn
	geom, _ := loc["geometry"].(map[string]any)
	coords, _ := geom["coordinates"].([]any)
	props, _ := loc["properties"].(map[string]any)
	if len(coords) < 2 || props == nil {
		return p, false, nil
	}
	lon, lat := num(coords[0]), num(coords[1])
	if lon == nil || lat == nil || !geo.ValidLonLat(*lon, *lat) {
		return p, false, nil
	}
	ts, err := geo.ParseTimestamp(props["timestamp"])
	if err != nil {
		return p, false, err
	}
	if ts == 0 || geo.NullIsland(*lon, *lat) {
		return p, false, nil
	}
	p = store.PointIn{
		Lon: *lon, Lat: *lat, Timestamp: ts,
		Battery:          batteryLevel(props["battery_level"]),
		BatteryStatus:    nil,
		Altitude:         num(props["altitude"]),
		Accuracy:         i32(props["horizontal_accuracy"]),
		VerticalAccuracy: i32(props["vertical_accuracy"]),
		Velocity:         str(props["speed"]),
		TrackerID:        str(props["device_id"]),
		SSID:             str(props["wifi"]),
		Course:           courseSafe(props["course"]),
		CourseAccuracy:   courseSafe(props["course_accuracy"]),
		MotionData:       motion(props),
		RawData:          loc,
	}
	if bs, ok := props["battery_state"].(string); ok {
		p.BatteryStatus = store.BatteryStatusCode(bs)
	}
	return p, true, nil
}

func courseSafe(v any) *float64 {
	f := num(v)
	if f == nil || math.Abs(*f) >= 1000 {
		return nil
	}
	return f
}

// OwnTracksPoint converts an OwnTracks HTTP payload. ok is false for
// waypoints and incomplete messages.
func OwnTracksPoint(m map[string]any) (store.PointIn, bool) {
	var p store.PointIn
	if t, _ := m["_type"].(string); t == "waypoint" {
		return p, false
	}
	lat, lon, tst := num(m["lat"]), num(m["lon"]), num(m["tst"])
	if lat == nil || lon == nil || tst == nil || !geo.ValidLonLat(*lon, *lat) || geo.NullIsland(*lon, *lat) {
		return p, false
	}
	p = store.PointIn{
		Lon: *lon, Lat: *lat, Timestamp: int64(*tst),
		Battery:          i32(m["batt"]),
		Ping:             str(m["p"]),
		Altitude:         num(m["alt"]),
		Accuracy:         i32(m["acc"]),
		VerticalAccuracy: i32(m["vac"]),
		SSID:             str(m["SSID"]),
		BSSID:            str(m["BSSID"]),
		TrackerID:        str(m["tid"]),
		InRIDs:           strList(m["inrids"]),
		InRegions:        strList(m["inregions"]),
		Topic:            str(m["topic"]),
		RawData:          m,
	}
	// OwnTracks reports km/h when sent over MQTT-style topics; Dawarich stores m/s.
	if v := num(m["vel"]); v != nil {
		s := strconv.FormatFloat(math.Round(*v*1000/3600*10)/10, 'f', -1, 64)
		if m["topic"] == nil {
			s = strconv.FormatFloat(*v, 'f', -1, 64)
		}
		p.Velocity = &s
	}
	bs := int32(0)
	if b := num(m["bs"]); b != nil && *b >= 1 && *b <= 3 {
		bs = int32(*b)
	}
	p.BatteryStatus = &bs
	trig := map[string]int32{"p": 1, "c": 2, "b": 3, "r": 4, "u": 5, "t": 6, "v": 7}
	t := int32(0)
	if s, ok := m["t"].(string); ok {
		t = trig[s]
	}
	p.Trigger = &t
	conn := int32(0)
	switch m["conn"] {
	case "w":
		conn = 1
	case "o":
		conn = 2
	case nil, "m":
		conn = 0
	default:
		conn = 4
	}
	p.Connection = &conn
	md := map[string]any{}
	if v, ok := m["m"]; ok {
		md["m"] = v
	}
	if v, ok := m["_type"]; ok {
		md["_type"] = v
	}
	p.MotionData = md
	return p, true
}

// TraccarPoint converts the Traccar / background-geolocation payloads.
func TraccarPoint(m map[string]any) (store.PointIn, bool) {
	var p store.PointIn
	loc, _ := m["location"].(map[string]any)
	var lat, lon *float64
	var tsv any
	if loc != nil {
		coords, _ := loc["coords"].(map[string]any)
		if coords != nil {
			lat, lon = num(coords["latitude"]), num(coords["longitude"])
			p.Accuracy, p.Altitude = i32(coords["accuracy"]), num(coords["altitude"])
			p.Velocity = str(coords["speed"])
		}
		if lat == nil {
			lat, lon = num(loc["latitude"]), num(loc["longitude"])
		}
		tsv = loc["timestamp"]
		if bat, ok := loc["battery"].(map[string]any); ok {
			if l := num(bat["level"]); l != nil {
				n := int32(*l * 100)
				p.Battery = &n
			}
		}
	}
	if lat == nil {
		lat, lon = num(m["lat"]), num(m["lon"])
		tsv = m["timestamp"]
		p.Accuracy, p.Altitude = i32(m["accuracy"]), num(m["altitude"])
		p.Velocity = str(m["speed"])
		if b := num(m["batt"]); b != nil {
			n := int32(*b)
			p.Battery = &n
		}
	}
	if lat == nil || lon == nil || !geo.ValidLonLat(*lon, *lat) || geo.NullIsland(*lon, *lat) {
		return p, false
	}
	ts, err := geo.ParseTimestamp(tsv)
	if err != nil || ts == 0 {
		return p, false
	}
	p.Lon, p.Lat, p.Timestamp = *lon, *lat, ts
	p.TrackerID = str(m["device_id"])
	if p.TrackerID == nil {
		p.TrackerID = str(m["id"])
	}
	md := map[string]any{}
	if act, ok := m["activity"].(map[string]any); ok {
		if t, ok := act["type"]; ok {
			md["activity"] = t
		}
	} else if loc != nil {
		if act, ok := loc["activity"].(map[string]any); ok {
			if t, ok := act["type"]; ok {
				md["activity"] = t
			}
		}
	}
	if loc != nil {
		if v, ok := loc["is_moving"]; ok {
			md["is_moving"] = v
		}
		if v, ok := loc["event"]; ok {
			md["event"] = v
		}
	}
	p.MotionData, p.RawData = md, m
	return p, true
}
