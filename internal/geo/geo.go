// Package geo holds small, dependency-free geographic helpers.
package geo

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

const earthRadiusM = 6371008.8

// NullIslandRadiusM mirrors Points::NullIsland::RADIUS_METERS.
const NullIslandRadiusM = 5000

// Haversine returns the great-circle distance in meters.
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusM * math.Asin(math.Min(1, math.Sqrt(a)))
}

// NullIsland reports whether a coordinate is within the broken-reading
// neighbourhood around (0,0).
func NullIsland(lon, lat float64) bool {
	return Haversine(lat, lon, 0, 0) <= NullIslandRadiusM
}

// ValidLonLat reports whether the coordinate is finite and in range.
func ValidLonLat(lon, lat float64) bool {
	return !math.IsNaN(lon) && !math.IsNaN(lat) && !math.IsInf(lon, 0) && !math.IsInf(lat, 0) &&
		lon >= -180 && lon <= 180 && lat >= -90 && lat <= 90
}

const (
	MinTimestamp = -2147483648
	MaxTimestamp = 2147483647
)

var ErrInvalidTimestamp = errors.New("Timestamp must be a date and time or Unix seconds")

var timeLayouts = []string{
	time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05 -0700",
	"2006-01-02 15:04:05 MST", "2006-01-02 15:04:05", "2006-01-02T15:04:05Z0700", "2006-01-02",
}

// ParseTimestamp accepts Unix seconds (as number or string) or a date-time
// string and returns Unix seconds. Mirrors Points::TimestampParser.
func ParseTimestamp(v any) (int64, error) {
	var s string
	switch t := v.(type) {
	case nil:
		return 0, nil
	case string:
		s = strings.TrimSpace(t)
	case float64:
		return checkTS(int64(t))
	case int64:
		return checkTS(t)
	case int:
		return checkTS(int64(t))
	default:
		return 0, ErrInvalidTimestamp
	}
	if s == "" {
		return 0, nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return checkTS(n)
	}
	for _, l := range timeLayouts {
		if tm, err := time.Parse(l, s); err == nil {
			return checkTS(tm.Unix())
		}
	}
	return 0, ErrInvalidTimestamp
}

func checkTS(n int64) (int64, error) {
	if n < MinTimestamp || n > MaxTimestamp {
		return 0, ErrInvalidTimestamp
	}
	return n, nil
}
