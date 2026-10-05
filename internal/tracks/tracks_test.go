package tracks

import (
	"testing"

	"github.com/sixface/beenthere/internal/store"
)

func TestSegment(t *testing.T) {
	mk := func(ts ...int64) []store.TrackPoint {
		var p []store.TrackPoint
		for _, t := range ts {
			p = append(p, store.TrackPoint{Timestamp: t})
		}
		return p
	}
	// gap 60s: [0,30,60] | [500] (dropped, single) | [1000,1030]
	segs := Segment(mk(0, 30, 60, 500, 1000, 1030), 60)
	if len(segs) != 2 || len(segs[0]) != 3 || len(segs[1]) != 2 {
		t.Fatalf("unexpected segments: %v", segs)
	}
	if len(Segment(nil, 60)) != 0 {
		t.Fatal("empty input")
	}
}

func TestComputeStats(t *testing.T) {
	a, b := 10.0, 25.0
	st := store.ComputeTrackStats([]store.TrackPoint{
		{Lat: 0, Lon: 10, Timestamp: 0, Altitude: &a},
		{Lat: 0, Lon: 10.01, Timestamp: 100, Altitude: &b},
	})
	if st.Distance < 1100 || st.Distance > 1120 || st.Duration != 100 || st.Gain != 15 || st.ElevMax != 25 {
		t.Fatalf("%+v", st)
	}
}
