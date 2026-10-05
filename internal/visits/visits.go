// Package visits detects stays ("visits") from point streams.
package visits

import (
	"context"
	"time"

	"github.com/sixface/beenthere/internal/geo"
	"github.com/sixface/beenthere/internal/store"
)

// Policy mirrors Visits::Detection::Policy.
type Policy struct {
	RadiusM   float64
	MinDwellS int64
	MinPoints int
	MergeGapS int64
	SweepGapS int64
}

func PolicyFor(u *store.User) Policy {
	return Policy{
		RadiusM:   float64(u.Setting("visit_radius_meters")),
		MinDwellS: int64(u.Setting("visit_min_duration_minutes")) * 60,
		MinPoints: u.Setting("visit_min_points"),
		MergeGapS: int64(u.Setting("merge_threshold_minutes")) * 60,
		SweepGapS: 3600,
	}
}

type P struct {
	ID        int64
	Lon, Lat  float64
	Timestamp int64
}

type Stay struct {
	Lon, Lat   float64
	Start, End int64
	PointIDs   []int64
}

// Detect finds stays in time-ordered points.
func Detect(pts []P, pol Policy) []Stay {
	var stays []Stay
	var cur []P
	var sumLon, sumLat float64
	flush := func() {
		if len(cur) >= pol.MinPoints && cur[len(cur)-1].Timestamp-cur[0].Timestamp >= pol.MinDwellS {
			s := Stay{Lon: sumLon / float64(len(cur)), Lat: sumLat / float64(len(cur)),
				Start: cur[0].Timestamp, End: cur[len(cur)-1].Timestamp}
			for _, p := range cur {
				s.PointIDs = append(s.PointIDs, p.ID)
			}
			stays = append(stays, s)
		}
		cur, sumLon, sumLat = nil, 0, 0
	}
	for _, p := range pts {
		if len(cur) > 0 {
			cLon, cLat := sumLon/float64(len(cur)), sumLat/float64(len(cur))
			if geo.Haversine(cLat, cLon, p.Lat, p.Lon) > pol.RadiusM || p.Timestamp-cur[len(cur)-1].Timestamp > pol.SweepGapS {
				flush()
			}
		}
		cur = append(cur, p)
		sumLon += p.Lon
		sumLat += p.Lat
	}
	flush()
	return mergeStays(stays, pol)
}

// mergeStays joins consecutive stays at the same place separated by short gaps.
func mergeStays(in []Stay, pol Policy) []Stay {
	var out []Stay
	for _, s := range in {
		if n := len(out); n > 0 {
			last := &out[n-1]
			if s.Start-last.End <= pol.MergeGapS && geo.Haversine(last.Lat, last.Lon, s.Lat, s.Lon) <= pol.RadiusM {
				wl, ws := float64(len(last.PointIDs)), float64(len(s.PointIDs))
				last.Lon = (last.Lon*wl + s.Lon*ws) / (wl + ws)
				last.Lat = (last.Lat*wl + s.Lat*ws) / (wl + ws)
				last.End = s.End
				last.PointIDs = append(last.PointIDs, s.PointIDs...)
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

// Run detects and persists suggested visits for [fromTS, toTS].
func Run(ctx context.Context, s *store.Store, u *store.User, fromTS, toTS int64) (int, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, ST_X(lonlat::geometry), ST_Y(lonlat::geometry), timestamp FROM points
		WHERE user_id=$1 AND anomaly IS NOT TRUE AND timestamp BETWEEN $2 AND $3 ORDER BY timestamp`, u.ID, fromTS, toTS)
	if err != nil {
		return 0, err
	}
	var pts []P
	for rows.Next() {
		var p P
		var ts int32
		if err := rows.Scan(&p.ID, &p.Lon, &p.Lat, &ts); err != nil {
			rows.Close()
			return 0, err
		}
		p.Timestamp = int64(ts)
		pts = append(pts, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	created := 0
	for _, st := range Detect(pts, PolicyFor(u)) {
		ok, err := s.PersistStay(ctx, u.ID, st.Lat, st.Lon, time.Unix(st.Start, 0).UTC(), time.Unix(st.End, 0).UTC(), st.PointIDs)
		if err != nil {
			return created, err
		}
		if ok {
			created++
		}
	}
	return created, nil
}
