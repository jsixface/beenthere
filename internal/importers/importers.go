// Package importers parses location-history files into points. Parsers
// stream: memory use is independent of file size.
package importers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/sixface/beenthere/internal/geo"
	"github.com/sixface/beenthere/internal/ingest"
	"github.com/sixface/beenthere/internal/store"
)

// Source ids match imports.source in the Rails schema.
const (
	SourceGoogleSemanticHistory = 0
	SourceOwnTracks             = 1
	SourceGoogleRecords         = 2
	SourceGooglePhoneTakeout    = 3
	SourceGPX                   = 4
	SourceGeoJSON               = 6
	SourceKML                   = 9
)

var SourceNames = map[string]int{
	"google_semantic_history": 0, "owntracks": 1, "google_records": 2, "google_phone_takeout": 3,
	"gpx": 4, "geojson": 6, "kml": 9,
}

// Emit receives each parsed point. Returning an error aborts the import.
type Emit func(store.PointIn) error

func valid(lon, lat float64, ts int64) bool {
	return ts != 0 && geo.ValidLonLat(lon, lat) && !geo.NullIsland(lon, lat)
}

// Detect guesses the source from the file name and leading bytes.
func Detect(name string, head []byte) (int, bool) {
	l := strings.ToLower(name)
	switch {
	case strings.HasSuffix(l, ".gpx"):
		return SourceGPX, true
	case strings.HasSuffix(l, ".kml"):
		return SourceKML, true
	case strings.HasSuffix(l, ".rec"):
		return SourceOwnTracks, true
	case strings.HasSuffix(l, ".geojson"):
		return SourceGeoJSON, true
	}
	h := string(head)
	switch {
	case strings.Contains(h, "<gpx"):
		return SourceGPX, true
	case strings.Contains(h, "<kml"):
		return SourceKML, true
	case strings.Contains(h, `"semanticSegments"`) || strings.Contains(h, `"rawSignals"`):
		return SourceGooglePhoneTakeout, true
	case strings.Contains(h, `"locations"`) && (strings.Contains(h, "latitudeE7") || strings.Contains(h, "timestampMs")):
		return SourceGoogleRecords, true
	case strings.Contains(h, `"FeatureCollection"`) || strings.Contains(h, `"Feature"`):
		return SourceGeoJSON, true
	case strings.Contains(h, `"_type"`) || strings.Contains(h, "\t*\t{"):
		return SourceOwnTracks, true
	}
	return 0, false
}

// Parse dispatches to the parser for source.
func Parse(source int, r io.Reader, emit Emit) error {
	switch source {
	case SourceGPX:
		return parseGPX(r, emit)
	case SourceKML:
		return parseKML(r, emit)
	case SourceGeoJSON:
		return parseGeoJSON(r, emit)
	case SourceOwnTracks:
		return parseOwnTracks(r, emit)
	case SourceGoogleRecords:
		return parseGoogleRecords(r, emit)
	case SourceGooglePhoneTakeout:
		return parseGoogleTakeout(r, emit)
	}
	return fmt.Errorf("unsupported import source %d", source)
}

// ---------- GPX ----------

func attr(se xml.StartElement, name string) string {
	for _, a := range se.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func parseTimeLoose(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	ts, err := geo.ParseTimestamp(s)
	return ts, err == nil && ts != 0
}

func parseGPX(r io.Reader, emit Emit) error {
	dec := xml.NewDecoder(bufio.NewReaderSize(r, 1<<16))
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("gpx: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || (se.Name.Local != "trkpt" && se.Name.Local != "rtept") {
			continue
		}
		lat, e1 := strconv.ParseFloat(attr(se, "lat"), 64)
		lon, e2 := strconv.ParseFloat(attr(se, "lon"), 64)
		var ele, speed, course *float64
		var ts int64
		depth := 1
		for depth > 0 {
			t, err := dec.Token()
			if err != nil {
				return fmt.Errorf("gpx: %w", err)
			}
			switch v := t.(type) {
			case xml.StartElement:
				depth++
				var text string
				switch v.Name.Local {
				case "ele", "time", "speed", "course":
					var sb strings.Builder
					for {
						c, err := dec.Token()
						if err != nil {
							return fmt.Errorf("gpx: %w", err)
						}
						if cd, ok := c.(xml.CharData); ok {
							sb.Write(cd)
						}
						if _, ok := c.(xml.EndElement); ok {
							break
						}
					}
					depth--
					text = strings.TrimSpace(sb.String())
				}
				switch v.Name.Local {
				case "ele":
					if f, err := strconv.ParseFloat(text, 64); err == nil {
						ele = &f
					}
				case "time":
					ts, _ = parseTimeLoose(text)
				case "speed":
					if f, err := strconv.ParseFloat(text, 64); err == nil {
						speed = &f
					}
				case "course":
					if f, err := strconv.ParseFloat(text, 64); err == nil {
						course = &f
					}
				}
			case xml.EndElement:
				depth--
			}
		}
		if e1 != nil || e2 != nil || !valid(lon, lat, ts) {
			continue
		}
		p := store.PointIn{Lon: lon, Lat: lat, Timestamp: ts, Altitude: ele, Course: course}
		if speed != nil {
			s := strconv.FormatFloat(*speed, 'f', -1, 64)
			p.Velocity = &s
		}
		if err := emit(p); err != nil {
			return err
		}
	}
}

// ---------- KML ----------

func parseKML(r io.Reader, emit Emit) error {
	dec := xml.NewDecoder(bufio.NewReaderSize(r, 1<<16))
	dec.Strict = false
	var whens []int64
	var coords [][3]float64
	var begin, end int64
	var pmCoords []string
	inPlacemark := false
	text := func() string {
		var sb strings.Builder
		for {
			c, err := dec.Token()
			if err != nil {
				return sb.String()
			}
			if cd, ok := c.(xml.CharData); ok {
				sb.Write(cd)
			}
			if _, ok := c.(xml.EndElement); ok {
				return strings.TrimSpace(sb.String())
			}
		}
	}
	parseCoord := func(s string) ([3]float64, bool) {
		f := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
		if len(f) < 2 {
			return [3]float64{}, false
		}
		var v [3]float64
		for i := 0; i < len(f) && i < 3; i++ {
			x, err := strconv.ParseFloat(f[i], 64)
			if err != nil {
				return v, false
			}
			v[i] = x
		}
		return v, true
	}
	flush := func() error {
		defer func() { whens, coords, begin, end, pmCoords = nil, nil, 0, 0, nil }()
		// gx:Track: parallel when / coord lists
		if len(whens) > 0 && len(whens) == len(coords) {
			for i := range whens {
				if valid(coords[i][0], coords[i][1], whens[i]) {
					a := coords[i][2]
					if err := emit(store.PointIn{Lon: coords[i][0], Lat: coords[i][1], Timestamp: whens[i], Altitude: &a}); err != nil {
						return err
					}
				}
			}
			return nil
		}
		// plain Placemark with a timestamp: Point, or LineString spread over TimeSpan
		ts := begin
		for _, c := range pmCoords {
			for _, tup := range strings.Fields(c) {
				cc, ok := parseCoord(tup)
				if ok && valid(cc[0], cc[1], ts) {
					a := cc[2]
					if err := emit(store.PointIn{Lon: cc[0], Lat: cc[1], Timestamp: ts, Altitude: &a}); err != nil {
						return err
					}
				}
			}
		}
		_ = end
		return nil
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("kml: %w", err)
		}
		switch v := tok.(type) {
		case xml.StartElement:
			switch v.Name.Local {
			case "Placemark":
				inPlacemark = true
			case "when":
				if ts, ok := parseTimeLoose(text()); ok {
					whens = append(whens, ts)
				}
			case "coord":
				if c, ok := parseCoord(text()); ok {
					coords = append(coords, c)
				}
			case "begin", "when_":
				if ts, ok := parseTimeLoose(text()); ok && begin == 0 {
					begin = ts
				}
			case "coordinates":
				if inPlacemark {
					pmCoords = append(pmCoords, text())
				}
			}
		case xml.EndElement:
			if v.Name.Local == "Placemark" {
				inPlacemark = false
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
}

// ---------- GeoJSON ----------

func parseGeoJSON(r io.Reader, emit Emit) error {
	dec := json.NewDecoder(bufio.NewReaderSize(r, 1<<16))
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("geojson: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		if d == '[' { // bare array of features
			for dec.More() {
				var f map[string]any
				if err := dec.Decode(&f); err != nil {
					return err
				}
				if err := emitFeature(f, emit); err != nil {
					return err
				}
			}
			return nil
		}
		return fmt.Errorf("geojson: unexpected top-level value")
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := kt.(string)
		if key != "features" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return err
			}
			continue
		}
		if _, err := dec.Token(); err != nil { // [
			return err
		}
		for dec.More() {
			var f map[string]any
			if err := dec.Decode(&f); err != nil {
				return err
			}
			if err := emitFeature(f, emit); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil { // ]
			return err
		}
	}
	return nil
}

func numAt(a []any, i int) (float64, bool) {
	if i >= len(a) {
		return 0, false
	}
	f, ok := a[i].(float64)
	return f, ok
}

func emitFeature(f map[string]any, emit Emit) error {
	geom, _ := f["geometry"].(map[string]any)
	props, _ := f["properties"].(map[string]any)
	typ, _ := geom["type"].(string)
	switch typ {
	case "Point":
		if pts, err := ingest.ParseGeoJSONLocations(mustJSON(f)); err == nil && len(pts) == 1 {
			return emit(pts[0])
		}
		// Plain Points with a flexible timestamp field.
		c, _ := geom["coordinates"].([]any)
		lon, ok1 := numAt(c, 0)
		lat, ok2 := numAt(c, 1)
		ts := tsFromProps(props)
		if ok1 && ok2 && valid(lon, lat, ts) {
			p := store.PointIn{Lon: lon, Lat: lat, Timestamp: ts}
			if alt, ok := numAt(c, 2); ok {
				p.Altitude = &alt
			}
			return emit(p)
		}
	case "LineString":
		c, _ := geom["coordinates"].([]any)
		var times []any
		if cp, ok := props["coordinateProperties"].(map[string]any); ok {
			times, _ = cp["times"].([]any)
		}
		for i, e := range c {
			xy, _ := e.([]any)
			lon, ok1 := numAt(xy, 0)
			lat, ok2 := numAt(xy, 1)
			if !ok1 || !ok2 || i >= len(times) {
				continue
			}
			ts, err := geo.ParseTimestamp(times[i])
			if err != nil || !valid(lon, lat, ts) {
				continue
			}
			p := store.PointIn{Lon: lon, Lat: lat, Timestamp: ts}
			if alt, ok := numAt(xy, 2); ok {
				p.Altitude = &alt
			}
			if err := emit(p); err != nil {
				return err
			}
		}
	}
	return nil
}

func tsFromProps(props map[string]any) int64 {
	for _, k := range []string{"timestamp", "time", "date", "datetime"} {
		if v, ok := props[k]; ok {
			if ts, err := geo.ParseTimestamp(v); err == nil && ts != 0 {
				return ts
			}
		}
	}
	return 0
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

// ---------- OwnTracks .rec ----------

func parseOwnTracks(r io.Reader, emit Emit) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if i := bytes.IndexByte(line, '{'); i > 0 {
			line = line[i:]
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			continue
		}
		if p, ok := ingest.OwnTracksPoint(m); ok {
			if err := emit(p); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

// ---------- Google ----------

func e7(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t / 1e7, true
	case json.Number:
		f, err := t.Float64()
		return f / 1e7, err == nil
	}
	return 0, false
}

func parseGoogleRecords(r io.Reader, emit Emit) error {
	dec := json.NewDecoder(bufio.NewReaderSize(r, 1<<16))
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("google records: %w", err)
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		if k, _ := kt.(string); k != "locations" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return err
			}
			continue
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
		for dec.More() {
			var loc map[string]any
			if err := dec.Decode(&loc); err != nil {
				return err
			}
			lat, ok1 := e7(loc["latitudeE7"])
			lon, ok2 := e7(loc["longitudeE7"])
			if !ok1 || !ok2 {
				continue
			}
			var ts int64
			if s, ok := loc["timestamp"].(string); ok {
				ts, _ = parseTimeLoose(s)
			} else if s, ok := loc["timestampMs"].(string); ok {
				if n, err := strconv.ParseInt(s, 10, 64); err == nil {
					ts = n / 1000
				}
			}
			if !valid(lon, lat, ts) {
				continue
			}
			p := store.PointIn{Lon: lon, Lat: lat, Timestamp: ts}
			if a, ok := loc["accuracy"].(float64); ok {
				n := int32(a)
				p.Accuracy = &n
			}
			if a, ok := loc["altitude"].(float64); ok {
				p.Altitude = &a
			}
			if a, ok := loc["velocity"].(float64); ok {
				s := strconv.FormatFloat(a, 'f', -1, 64)
				p.Velocity = &s
			}
			tid := "google-maps-timeline-export"
			p.TrackerID = &tid
			if err := emit(p); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
	}
	return nil
}

// parseGeoString handles Takeout "48.1°, 11.5°" style coordinates.
func parseGeoString(s string) (lat, lon float64, ok bool) {
	s = strings.NewReplacer("°", "", "geo:", "").Replace(s)
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	a, e1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	b, e2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	return a, b, e1 == nil && e2 == nil
}

// parseGoogleTakeout handles the on-device "Timeline.json"/phone export:
// semanticSegments[].timelinePath[] and rawSignals[].position.
func parseGoogleTakeout(r io.Reader, emit Emit) error {
	dec := json.NewDecoder(bufio.NewReaderSize(r, 1<<16))
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("google takeout: %w", err)
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := kt.(string)
		if key != "semanticSegments" && key != "rawSignals" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return err
			}
			continue
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
		for dec.More() {
			var seg map[string]any
			if err := dec.Decode(&seg); err != nil {
				return err
			}
			if key == "rawSignals" {
				pos, _ := seg["position"].(map[string]any)
				if pos == nil {
					continue
				}
				lat, lon, ok := parseGeoString(fmt.Sprint(pos["LatLng"]))
				ts, _ := parseTimeLoose(fmt.Sprint(pos["timestamp"]))
				if ok && valid(lon, lat, ts) {
					if err := emit(store.PointIn{Lon: lon, Lat: lat, Timestamp: ts}); err != nil {
						return err
					}
				}
				continue
			}
			tp, _ := seg["timelinePath"].([]any)
			for _, e := range tp {
				m, _ := e.(map[string]any)
				lat, lon, ok := parseGeoString(fmt.Sprint(m["point"]))
				ts, _ := parseTimeLoose(fmt.Sprint(m["time"]))
				if ok && valid(lon, lat, ts) {
					if err := emit(store.PointIn{Lon: lon, Lat: lat, Timestamp: ts}); err != nil {
						return err
					}
				}
			}
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
	}
	return nil
}

var _ = math.Pi
var _ = time.Second
