package store

import (
	"context"
	"encoding/json"
	"time"
)

type TripOut struct {
	ID               int64           `json:"id"`
	Name             string          `json:"name"`
	StartedAt        time.Time       `json:"started_at"`
	EndedAt          time.Time       `json:"ended_at"`
	Distance         *int64          `json:"distance"`
	Description      string          `json:"description"`
	VisitedCountries json.RawMessage `json:"visited_countries"`
	Path             json.RawMessage `json:"path"`
	PointsCount      int             `json:"points_count"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

const tripSelect = `SELECT t.id, t.name, t.started_at, t.ended_at, t.distance, coalesce(rt.body,''),
	coalesce(t.visited_countries,'[]'::jsonb), coalesce(ST_AsGeoJSON(t.path,6)::jsonb,'null'::jsonb),
	(SELECT count(*) FROM points p WHERE p.user_id=t.user_id AND p.anomaly IS NOT TRUE AND p.timestamp BETWEEN extract(epoch FROM t.started_at)::int AND extract(epoch FROM t.ended_at)::int),
	t.created_at, t.updated_at FROM trips t
	LEFT JOIN action_text_rich_texts rt ON rt.record_type='Trip' AND rt.record_id=t.id AND rt.name='description'`

func scanTrip(row interface{ Scan(...any) error }) (*TripOut, error) {
	var t TripOut
	if err := row.Scan(&t.ID, &t.Name, &t.StartedAt, &t.EndedAt, &t.Distance, &t.Description, &t.VisitedCountries,
		&t.Path, &t.PointsCount, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return &t, nil
}

func (s *Store) ListTrips(ctx context.Context, userID int64) ([]*TripOut, error) {
	rows, err := s.Pool.Query(ctx, tripSelect+` WHERE t.user_id=$1 ORDER BY t.started_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*TripOut{}
	for rows.Next() {
		t, err := scanTrip(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetTrip(ctx context.Context, userID, id int64) (*TripOut, error) {
	return scanTrip(s.Pool.QueryRow(ctx, tripSelect+` WHERE t.user_id=$1 AND t.id=$2`, userID, id))
}

func (s *Store) setTripDescription(ctx context.Context, id int64, desc string) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO action_text_rich_texts (name,body,record_type,record_id,created_at,updated_at)
		VALUES ('description',$2,'Trip',$1,now(),now())
		ON CONFLICT (record_type, record_id, name) DO UPDATE SET body=excluded.body, updated_at=now()`, id, desc)
	return err
}

func (s *Store) CreateTrip(ctx context.Context, userID int64, name string, from, to time.Time, desc string) (*TripOut, error) {
	var id int64
	if err := s.Pool.QueryRow(ctx, `INSERT INTO trips (user_id,name,started_at,ended_at,created_at,updated_at)
		VALUES ($1,$2,$3,$4,now(),now()) RETURNING id`, userID, name, from, to).Scan(&id); err != nil {
		return nil, err
	}
	if desc != "" {
		if err := s.setTripDescription(ctx, id, desc); err != nil {
			return nil, err
		}
	}
	if err := s.RecalculateTrip(ctx, userID, id); err != nil {
		return nil, err
	}
	return s.GetTrip(ctx, userID, id)
}

func (s *Store) UpdateTrip(ctx context.Context, userID, id int64, name *string, from, to *time.Time, desc *string) (*TripOut, error) {
	ct, err := s.Pool.Exec(ctx, `UPDATE trips SET name=coalesce($3,name), started_at=coalesce($4,started_at),
		ended_at=coalesce($5,ended_at), updated_at=now() WHERE user_id=$1 AND id=$2`, userID, id, name, from, to)
	if err != nil {
		return nil, err
	}
	if ct.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	if desc != nil {
		if err := s.setTripDescription(ctx, id, *desc); err != nil {
			return nil, err
		}
	}
	if from != nil || to != nil {
		if err := s.RecalculateTrip(ctx, userID, id); err != nil {
			return nil, err
		}
	}
	return s.GetTrip(ctx, userID, id)
}

func (s *Store) DeleteTrip(ctx context.Context, userID, id int64) error {
	return s.Tx(ctx, func(tx pgxTx) error {
		for _, q := range []string{
			`DELETE FROM action_text_rich_texts WHERE record_type='Trip' AND record_id=$1`,
			`DELETE FROM notes WHERE attachable_type='Trip' AND attachable_id=$1`,
			`DELETE FROM planned_day_notes WHERE planned_day_id IN (SELECT id FROM planned_days WHERE trip_id=$1)`,
			`DELETE FROM planned_stops WHERE planned_day_id IN (SELECT id FROM planned_days WHERE trip_id=$1)`,
			`DELETE FROM planned_reservations WHERE trip_id=$1`,
			`DELETE FROM planned_days WHERE trip_id=$1`,
			`DELETE FROM planned_accommodations WHERE trip_id=$1`,
			`DELETE FROM planned_travellers WHERE trip_id=$1`,
			`DELETE FROM planned_unplanned_places WHERE trip_id=$1`,
			`DELETE FROM shared_links WHERE resource_type=0 AND resource_id=$1`,
		} {
			if _, err := tx.Exec(ctx, q, id); err != nil {
				return err
			}
		}
		ct, err := tx.Exec(ctx, `DELETE FROM trips WHERE id=$1 AND user_id=$2`, id, userID)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// RecalculateTrip refreshes path, distance and visited countries from the
// points inside the trip's time window.
func (s *Store) RecalculateTrip(ctx context.Context, userID, id int64) error {
	_, err := s.Pool.Exec(ctx, `
WITH t AS (SELECT id, user_id, extract(epoch FROM started_at)::int AS s, extract(epoch FROM ended_at)::int AS e FROM trips WHERE id=$1 AND user_id=$2),
line AS (
  SELECT ST_MakeLine(p.lonlat::geometry ORDER BY p.timestamp) AS g, count(*) AS n
  FROM points p, t WHERE p.user_id=t.user_id AND p.anomaly IS NOT TRUE AND p.timestamp BETWEEN t.s AND t.e),
countries AS (
  SELECT coalesce(jsonb_agg(DISTINCT p.country_name) FILTER (WHERE p.country_name IS NOT NULL), '[]'::jsonb) AS c
  FROM points p, t WHERE p.user_id=t.user_id AND p.anomaly IS NOT TRUE AND p.timestamp BETWEEN t.s AND t.e)
UPDATE trips SET
  path = CASE WHEN (SELECT n FROM line) >= 2 THEN ST_SetSRID((SELECT g FROM line),4326) END,
  distance = CASE WHEN (SELECT n FROM line) >= 2 THEN round(ST_Length((SELECT g FROM line)::geography))::bigint ELSE 0 END,
  visited_countries = (SELECT c FROM countries), last_recalculated_at = now(), updated_at = now()
WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}
