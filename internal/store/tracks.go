package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sixface/beenthere/internal/geo"
)

// TrackPoint is the minimal point data needed to build tracks.
type TrackPoint struct {
	ID        int64
	Lon, Lat  float64
	Timestamp int64
	Altitude  *float64
	TrackerID *string
}

type Track struct {
	ID            int64
	StartAt       time.Time
	EndAt         time.Time
	Distance      int64
	AvgSpeed      float64
	Duration      int
	TrackerID     *string
	ElevationGain int
	ElevationLoss int
	ElevationMax  int
	ElevationMin  int
	DominantMode  int
	Revision      int32
	Geometry      json.RawMessage
}

// TrackStats are the derived metrics of a point sequence (Tracks::TrackBuilder).
type TrackStats struct {
	Distance                     int64
	Duration                     int
	AvgSpeedKmh                  float64
	Gain, Loss, ElevMax, ElevMin int
}

const maxTrackDistanceM = 100_000_000

// ComputeTrackStats calculates distance, speed and elevation for ordered points.
func ComputeTrackStats(pts []TrackPoint) TrackStats {
	var st TrackStats
	if len(pts) == 0 {
		return st
	}
	var dist float64
	for i := 1; i < len(pts); i++ {
		dist += geo.Haversine(pts[i-1].Lat, pts[i-1].Lon, pts[i].Lat, pts[i].Lon)
	}
	if dist > maxTrackDistanceM {
		dist = maxTrackDistanceM
	}
	st.Distance = int64(dist + 0.5)
	st.Duration = int(pts[len(pts)-1].Timestamp - pts[0].Timestamp)
	if st.Duration > 0 {
		st.AvgSpeedKmh = float64(st.Distance) / float64(st.Duration) * 3.6
	}
	first := true
	var prev float64
	for _, p := range pts {
		if p.Altitude == nil {
			continue
		}
		a := *p.Altitude
		if first {
			st.ElevMax, st.ElevMin = int(a), int(a)
			first = false
		} else {
			if d := a - prev; d > 0 {
				st.Gain += int(d)
			} else {
				st.Loss += int(-d)
			}
			if int(a) > st.ElevMax {
				st.ElevMax = int(a)
			}
			if int(a) < st.ElevMin {
				st.ElevMin = int(a)
			}
		}
		prev = a
	}
	return st
}

func lineWKT(pts []TrackPoint) string {
	b := make([]byte, 0, len(pts)*24+24)
	b = append(b, "LINESTRING("...)
	for i, p := range pts {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, fmt.Sprintf("%.6f %.6f", p.Lon, p.Lat)...)
	}
	return string(append(b, ')'))
}

// InsertTrack creates a track from ordered points and attaches the points.
// It returns 0 if an identical track already exists.
func InsertTrack(ctx context.Context, tx pgx.Tx, userID int64, pts []TrackPoint, importID *int64) (int64, error) {
	if len(pts) < 2 {
		return 0, nil
	}
	st := ComputeTrackStats(pts)
	tracker := pts[0].TrackerID
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO tracks (user_id, tracker_id, start_at, end_at, original_path, distance, duration,
		avg_speed, elevation_gain, elevation_loss, elevation_max, elevation_min, import_id, created_at, updated_at)
		VALUES ($1,$2,to_timestamp($3),to_timestamp($4),ST_GeomFromText($5,4326),$6,$7,$8,$9,$10,$11,$12,$13,now(),now())
		ON CONFLICT (user_id, COALESCE(tracker_id, ''::character varying), start_at, end_at) DO NOTHING
		RETURNING id`,
		userID, tracker, pts[0].Timestamp, pts[len(pts)-1].Timestamp, lineWKT(pts), st.Distance, st.Duration,
		st.AvgSpeedKmh, st.Gain, st.Loss, st.ElevMax, st.ElevMin, importID).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	ids := make([]int64, len(pts))
	for i, p := range pts {
		ids[i] = p.ID
	}
	if _, err := tx.Exec(ctx, `UPDATE points SET track_id = $1 WHERE id = ANY($2)`, id, ids); err != nil {
		return 0, err
	}
	return id, nil
}

// RefreshTrackFromPoints recomputes a track from its remaining points, or
// deletes it when fewer than two remain.
func RefreshTrackFromPoints(ctx context.Context, tx pgx.Tx, userID, trackID int64) error {
	rows, err := tx.Query(ctx, `SELECT id, ST_X(lonlat::geometry), ST_Y(lonlat::geometry), timestamp,
		coalesce(altitude_decimal, altitude), tracker_id FROM points
		WHERE track_id = $1 AND user_id = $2 ORDER BY timestamp`, trackID, userID)
	if err != nil {
		return err
	}
	var pts []TrackPoint
	for rows.Next() {
		var p TrackPoint
		var ts int32
		var alt *float64
		if err := rows.Scan(&p.ID, &p.Lon, &p.Lat, &ts, &alt, &p.TrackerID); err != nil {
			rows.Close()
			return err
		}
		p.Timestamp, p.Altitude = int64(ts), alt
		pts = append(pts, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(pts) < 2 {
		if _, err := tx.Exec(ctx, `DELETE FROM track_segments WHERE track_id = $1`, trackID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE points SET track_id = NULL WHERE track_id = $1`, trackID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM tracks WHERE id = $1 AND user_id = $2`, trackID, userID)
		return err
	}
	st := ComputeTrackStats(pts)
	_, err = tx.Exec(ctx, `UPDATE tracks SET start_at = to_timestamp($3), end_at = to_timestamp($4),
		original_path = ST_GeomFromText($5,4326), distance = $6, duration = $7, avg_speed = $8,
		elevation_gain = $9, elevation_loss = $10, elevation_max = $11, elevation_min = $12,
		lock_version = lock_version + 1, updated_at = now() WHERE id = $1 AND user_id = $2`,
		trackID, userID, pts[0].Timestamp, pts[len(pts)-1].Timestamp, lineWKT(pts), st.Distance, st.Duration,
		st.AvgSpeedKmh, st.Gain, st.Loss, st.ElevMax, st.ElevMin)
	return err
}

// UntrackedPoints streams points with no track for a user in time order.
func (s *Store) UntrackedPoints(ctx context.Context, userID int64, fromTS int64, fn func(TrackPoint) error) error {
	rows, err := s.Pool.Query(ctx, `SELECT id, ST_X(lonlat::geometry), ST_Y(lonlat::geometry), timestamp,
		coalesce(altitude_decimal, altitude), tracker_id FROM points
		WHERE user_id = $1 AND track_id IS NULL AND anomaly IS NOT TRUE AND timestamp >= $2
		ORDER BY coalesce(tracker_id,''), timestamp`, userID, fromTS)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var p TrackPoint
		var ts int32
		var alt *float64
		if err := rows.Scan(&p.ID, &p.Lon, &p.Lat, &ts, &alt, &p.TrackerID); err != nil {
			return err
		}
		p.Timestamp, p.Altitude = int64(ts), alt
		if err := fn(p); err != nil {
			return err
		}
	}
	return rows.Err()
}

const trackSelect = `SELECT id, start_at, end_at, coalesce(distance,0), coalesce(avg_speed,0), coalesce(duration,0), tracker_id,
	coalesce(elevation_gain,0), coalesce(elevation_loss,0), coalesce(elevation_max,0), coalesce(elevation_min,0),
	coalesce(dominant_mode,0), ST_AsGeoJSON(original_path, 6)::jsonb, lock_version FROM tracks`

func scanTrack(row interface{ Scan(...any) error }) (*Track, error) {
	var t Track
	if err := row.Scan(&t.ID, &t.StartAt, &t.EndAt, &t.Distance, &t.AvgSpeed, &t.Duration, &t.TrackerID,
		&t.ElevationGain, &t.ElevationLoss, &t.ElevationMax, &t.ElevationMin, &t.DominantMode, &t.Geometry, &t.Revision); err != nil {
		return nil, notFound(err)
	}
	return &t, nil
}

func (s *Store) ListTracks(ctx context.Context, userID int64, from, to *time.Time, limit, offset int) ([]Track, int, error) {
	where, args := "user_id = $1", []any{userID}
	if from != nil && to != nil {
		args = append(args, *from, *to)
		where += " AND end_at >= $2 AND start_at <= $3"
	}
	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := s.Pool.Query(ctx, fmt.Sprintf(`%s WHERE %s ORDER BY start_at DESC LIMIT $%d OFFSET $%d`,
		trackSelect, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Track{}
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *t)
	}
	return out, total, rows.Err()
}

func (s *Store) GetTrack(ctx context.Context, userID, id int64) (*Track, error) {
	return scanTrack(s.Pool.QueryRow(ctx, trackSelect+` WHERE user_id=$1 AND id=$2`, userID, id))
}
