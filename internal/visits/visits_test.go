package visits

import "testing"

func TestDetect(t *testing.T) {
	pol := Policy{RadiusM: 100, MinDwellS: 300, MinPoints: 3, MergeGapS: 900, SweepGapS: 3600}
	var pts []P
	// stay at A for 10 minutes
	for i := 0; i < 6; i++ {
		pts = append(pts, P{ID: int64(i), Lon: 13.4, Lat: 52.5 + float64(i)*0.00001, Timestamp: int64(i * 120)})
	}
	// travel
	pts = append(pts, P{ID: 100, Lon: 13.5, Lat: 52.6, Timestamp: 900})
	// short blip at B (too short)
	pts = append(pts, P{ID: 101, Lon: 13.7, Lat: 52.7, Timestamp: 1000}, P{ID: 102, Lon: 13.7, Lat: 52.7, Timestamp: 1060})
	got := Detect(pts, pol)
	if len(got) != 1 || len(got[0].PointIDs) != 6 || got[0].End-got[0].Start != 600 {
		t.Fatalf("%+v", got)
	}
}

func TestMerge(t *testing.T) {
	pol := Policy{RadiusM: 100, MinDwellS: 60, MinPoints: 2, MergeGapS: 900, SweepGapS: 3600}
	pts := []P{{1, 1, 1, 0}, {2, 1, 1, 100}, {3, 1.0001, 1, 1000}, {4, 1.0001, 1, 1100}}
	// gap between second and third is 900s: same cluster is broken only by distance/sweep, so a single stay results
	got := Detect(pts, pol)
	if len(got) != 1 || got[0].End != 1100 {
		t.Fatalf("%+v", got)
	}
}
