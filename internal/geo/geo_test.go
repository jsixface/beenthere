package geo

import (
	"math"
	"testing"
)

func TestHaversine(t *testing.T) {
	// London -> Paris is roughly 343 km.
	d := Haversine(51.5074, -0.1278, 48.8566, 2.3522)
	if math.Abs(d-343500) > 2000 {
		t.Fatalf("got %.0f", d)
	}
	if Haversine(10, 10, 10, 10) != 0 {
		t.Fatal("zero distance expected")
	}
}

func TestNullIsland(t *testing.T) {
	if !NullIsland(0, 0) || !NullIsland(0.01, -0.02) {
		t.Fatal("near zero should be null island")
	}
	if NullIsland(0.2, 0.2) {
		t.Fatal("~31km away should not be null island")
	}
}

func TestParseTimestamp(t *testing.T) {
	cases := []struct {
		in   any
		want int64
		err  bool
	}{
		{"1700000000", 1700000000, false},
		{float64(1700000000), 1700000000, false},
		{"2023-11-14T22:13:20Z", 1700000000, false},
		{"2023-11-14T22:13:20+00:00", 1700000000, false},
		{nil, 0, false},
		{"", 0, false},
		{"nonsense", 0, true},
		{"99999999999", 0, true},
	}
	for _, c := range cases {
		got, err := ParseTimestamp(c.in)
		if (err != nil) != c.err || got != c.want {
			t.Errorf("%v: got %d, %v", c.in, got, err)
		}
	}
}
