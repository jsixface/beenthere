package importers

import (
	"strings"
	"testing"

	"github.com/sixface/beenthere/internal/store"
)

func collect(t *testing.T, src int, in string) []store.PointIn {
	t.Helper()
	var out []store.PointIn
	if err := Parse(src, strings.NewReader(in), func(p store.PointIn) error { out = append(out, p); return nil }); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGPX(t *testing.T) {
	pts := collect(t, SourceGPX, `<?xml version="1.0"?><gpx><trk><trkseg>
	<trkpt lat="52.5" lon="13.4"><ele>34.5</ele><time>2023-11-14T22:13:20Z</time><speed>1.2</speed></trkpt>
	<trkpt lat="52.6" lon="13.5"><time>2023-11-14T22:14:20Z</time></trkpt>
	<trkpt lat="0" lon="0"><time>2023-11-14T22:15:20Z</time></trkpt>
	</trkseg></trk></gpx>`)
	if len(pts) != 2 || pts[0].Timestamp != 1700000000 || *pts[0].Altitude != 34.5 || *pts[0].Velocity != "1.2" {
		t.Fatalf("%+v", pts)
	}
}

func TestGeoJSON(t *testing.T) {
	pts := collect(t, SourceGeoJSON, `{"type":"FeatureCollection","features":[
	 {"type":"Feature","geometry":{"type":"Point","coordinates":[13.4,52.5]},"properties":{"timestamp":1700000000}},
	 {"type":"Feature","geometry":{"type":"LineString","coordinates":[[13.4,52.5,10],[13.5,52.6,11]]},
	  "properties":{"coordinateProperties":{"times":["2023-11-14T22:13:20Z","2023-11-14T22:14:20Z"]}}}]}`)
	if len(pts) != 3 {
		t.Fatalf("%d %+v", len(pts), pts)
	}
}

func TestKMLTrack(t *testing.T) {
	pts := collect(t, SourceKML, `<kml xmlns:gx="x"><Placemark><gx:Track>
	<when>2023-11-14T22:13:20Z</when><when>2023-11-14T22:14:20Z</when>
	<gx:coord>13.4 52.5 10</gx:coord><gx:coord>13.5 52.6 11</gx:coord></gx:Track></Placemark></kml>`)
	if len(pts) != 2 || pts[1].Timestamp != 1700000060 {
		t.Fatalf("%+v", pts)
	}
}

func TestOwnTracksRec(t *testing.T) {
	pts := collect(t, SourceOwnTracks, "2023-11-14T22:13:20Z\t*\t{\"_type\":\"location\",\"lat\":52.5,\"lon\":13.4,\"tst\":1700000000}\n")
	if len(pts) != 1 || pts[0].Timestamp != 1700000000 {
		t.Fatalf("%+v", pts)
	}
}

func TestGoogleRecords(t *testing.T) {
	pts := collect(t, SourceGoogleRecords, `{"locations":[{"latitudeE7":525000000,"longitudeE7":134000000,"timestamp":"2023-11-14T22:13:20.123Z","accuracy":12}]}`)
	if len(pts) != 1 || pts[0].Lat != 52.5 || *pts[0].Accuracy != 12 {
		t.Fatalf("%+v", pts)
	}
}

func TestTakeout(t *testing.T) {
	pts := collect(t, SourceGooglePhoneTakeout, `{"semanticSegments":[{"timelinePath":[{"point":"52.5°, 13.4°","time":"2023-11-14T22:13:20Z"}]}]}`)
	if len(pts) != 1 || pts[0].Lon != 13.4 {
		t.Fatalf("%+v", pts)
	}
}

func TestDetect(t *testing.T) {
	if s, ok := Detect("x.gpx", nil); !ok || s != SourceGPX {
		t.Fatal()
	}
	if s, ok := Detect("Records.json", []byte(`{"locations":[{"latitudeE7":1,"timestampMs":"1"}]}`)); !ok || s != SourceGoogleRecords {
		t.Fatal()
	}
}
