package store

import (
	"context"
	"time"
)

type Area struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Radius    int       `json:"radius"`
	UserID    int64     `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const areaCols = `id, name, latitude::float8, longitude::float8, radius, user_id, created_at, updated_at`

func scanArea(row interface{ Scan(...any) error }) (*Area, error) {
	var a Area
	if err := row.Scan(&a.ID, &a.Name, &a.Latitude, &a.Longitude, &a.Radius, &a.UserID, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return &a, nil
}

func (s *Store) ListAreas(ctx context.Context, userID int64) ([]*Area, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+areaCols+` FROM areas WHERE user_id=$1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Area{}
	for rows.Next() {
		a, err := scanArea(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetArea(ctx context.Context, userID, id int64) (*Area, error) {
	return scanArea(s.Pool.QueryRow(ctx, `SELECT `+areaCols+` FROM areas WHERE user_id=$1 AND id=$2`, userID, id))
}

func (s *Store) CreateArea(ctx context.Context, userID int64, name string, lat, lon float64, radius int) (*Area, error) {
	return scanArea(s.Pool.QueryRow(ctx, `INSERT INTO areas (user_id,name,latitude,longitude,radius,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,now(),now()) RETURNING `+areaCols, userID, name, lat, lon, radius))
}

func (s *Store) UpdateArea(ctx context.Context, userID, id int64, name string, lat, lon float64, radius int) (*Area, error) {
	return scanArea(s.Pool.QueryRow(ctx, `UPDATE areas SET name=$3,latitude=$4,longitude=$5,radius=$6,updated_at=now()
		WHERE user_id=$1 AND id=$2 RETURNING `+areaCols, userID, id, name, lat, lon, radius))
}

func (s *Store) DeleteArea(ctx context.Context, userID, id int64) error {
	ct, err := s.Pool.Exec(ctx, `DELETE FROM areas WHERE user_id=$1 AND id=$2`, userID, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
