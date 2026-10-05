package stats

import "testing"

func sp(s string) *string { return &s }

func TestToponyms(t *testing.T) {
	pts := []GeoPoint{
		{1, 0, sp("Berlin"), sp("Germany"), 1},
		{2, 1800, sp("Berlin"), sp("Germany"), 1},
		{3, 3600, sp("Berlin"), sp("Germany"), 1},
		{4, 4000, sp("Paris"), sp("France"), 1}, // too short a stay
		{5, 4100, sp("Paris"), sp("France"), 1},
	}
	got := Toponyms(pts, 30)
	if len(got) != 2 || got[0].Country != "Germany" || len(got[0].Cities) != 1 || got[0].Cities[0].StayedFor != 60 {
		t.Fatalf("%+v", got)
	}
	if len(got[1].Cities) != 0 {
		t.Fatalf("paris should be filtered: %+v", got[1])
	}
}

func TestMonthsInRange(t *testing.T) {
	m := MonthsInRange(1704067200, 1709251200, "UTC") // 2024-01-01 .. 2024-03-01
	if len(m) != 3 || m[0] != [2]int{2024, 1} || m[2] != [2]int{2024, 3} {
		t.Fatalf("%v", m)
	}
}
