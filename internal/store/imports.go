package store

import (
	"context"
	"time"
)

var ImportStatuses = []string{"created", "processing", "completed", "failed", "deleting"}
var ImportSources = []string{"google_semantic_history", "owntracks", "google_records", "google_phone_takeout", "gpx",
	"immich_api", "geojson", "photoprism_api", "user_data_archive", "kml", "csv", "tcx", "fit", "polarsteps",
	"google_photos", "mobile_photo_library"}

type ImportOut struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Source       *string   `json:"source"`
	Status       string    `json:"status"`
	PointsCount  int       `json:"points_count"`
	Processed    int       `json:"processed"`
	RawPoints    int       `json:"raw_points"`
	Doubles      int       `json:"doubles"`
	ErrorMessage *string   `json:"error_message"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

const importSelect = `SELECT id, name, source, status, coalesce(points_count,0), coalesce(processed,0), coalesce(raw_points,0), coalesce(doubles,0),
	error_message, created_at, updated_at FROM imports`

func scanImport(row interface{ Scan(...any) error }) (*ImportOut, error) {
	var i ImportOut
	var src *int
	var st int
	if err := row.Scan(&i.ID, &i.Name, &src, &st, &i.PointsCount, &i.Processed, &i.RawPoints, &i.Doubles, &i.ErrorMessage, &i.CreatedAt, &i.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	if src != nil && *src >= 0 && *src < len(ImportSources) {
		i.Source = &ImportSources[*src]
	}
	if st >= 0 && st < len(ImportStatuses) {
		i.Status = ImportStatuses[st]
	}
	return &i, nil
}

func (s *Store) ListImports(ctx context.Context, userID int64, limit, offset int) ([]*ImportOut, int, error) {
	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM imports WHERE user_id=$1`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.Pool.Query(ctx, importSelect+` WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*ImportOut{}
	for rows.Next() {
		i, err := scanImport(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, i)
	}
	return out, total, rows.Err()
}

func (s *Store) GetImport(ctx context.Context, userID, id int64) (*ImportOut, error) {
	return scanImport(s.Pool.QueryRow(ctx, importSelect+` WHERE user_id=$1 AND id=$2`, userID, id))
}

// CreateImport inserts an import row, picking a unique name for the user.
func (s *Store) CreateImport(ctx context.Context, userID int64, name string, source int) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `INSERT INTO imports (user_id,name,source,status,created_at,updated_at)
		VALUES ($1,$2,$3,0,now(),now()) RETURNING id`, userID, name, source).Scan(&id)
	return id, err
}

func (s *Store) ImportNameTaken(ctx context.Context, userID int64, name string) (bool, error) {
	var b bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM imports WHERE user_id=$1 AND name=$2)`, userID, name).Scan(&b)
	return b, err
}

func (s *Store) MarkImportProcessing(ctx context.Context, id int64) error {
	_, err := s.Pool.Exec(ctx, `UPDATE imports SET status=1, processing_started_at=now(), updated_at=now() WHERE id=$1`, id)
	return err
}

func (s *Store) UpdateImportProgress(ctx context.Context, id int64, processed, raw, doubles int) error {
	_, err := s.Pool.Exec(ctx, `UPDATE imports SET processed=$2, raw_points=$3, doubles=$4, updated_at=now() WHERE id=$1`, id, processed, raw, doubles)
	return err
}

func (s *Store) FinishImport(ctx context.Context, id int64, errMsg string) error {
	if errMsg != "" {
		_, err := s.Pool.Exec(ctx, `UPDATE imports SET status=3, error_message=$2, updated_at=now() WHERE id=$1`, id, errMsg)
		return err
	}
	_, err := s.Pool.Exec(ctx, `UPDATE imports SET status=2, points_count=(SELECT count(*) FROM points WHERE import_id=$1), updated_at=now() WHERE id=$1`, id)
	return err
}

// DeleteImport removes an import and its points.
func (s *Store) DeleteImport(ctx context.Context, userID, id int64) error {
	return s.Tx(ctx, func(tx pgxTx) error {
		var n int64
		if err := tx.QueryRow(ctx, `SELECT id FROM imports WHERE id=$1 AND user_id=$2`, id, userID).Scan(&n); err != nil {
			return notFound(err)
		}
		var tracks []int64
		rows, err := tx.Query(ctx, `SELECT DISTINCT track_id FROM points WHERE import_id=$1 AND track_id IS NOT NULL`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var t int64
			if err := rows.Scan(&t); err != nil {
				rows.Close()
				return err
			}
			tracks = append(tracks, t)
		}
		rows.Close()
		ct, err := tx.Exec(ctx, `DELETE FROM points WHERE import_id=$1 AND user_id=$2`, id, userID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET points_count = greatest(points_count - $2, 0) WHERE id=$1`, userID, ct.RowsAffected()); err != nil {
			return err
		}
		for _, t := range tracks {
			if err := RefreshTrackFromPoints(ctx, tx, userID, t); err != nil {
				return err
			}
		}
		for _, q := range []string{`UPDATE visits SET import_id=NULL WHERE import_id=$1`, `UPDATE places SET import_id=NULL WHERE import_id=$1`,
			`UPDATE tracks SET import_id=NULL WHERE import_id=$1`} {
			if _, err := tx.Exec(ctx, q, id); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM imports WHERE id=$1 AND user_id=$2`, id, userID)
		return err
	})
}

// PointsMinMax returns the timestamp range of a user's import.
func (s *Store) ImportRange(ctx context.Context, importID int64) (lo, hi int64, err error) {
	var a, b *int32
	err = s.Pool.QueryRow(ctx, `SELECT min(timestamp), max(timestamp) FROM points WHERE import_id=$1`, importID).Scan(&a, &b)
	if a != nil && b != nil {
		lo, hi = int64(*a), int64(*b)
	}
	return
}

// ExportRows streams points for export in timestamp order.
func (s *Store) ExportRows(ctx context.Context, userID, from, to int64, fn func(lon, lat float64, ts int64, alt *float64, vel *string, extra map[string]any) error) error {
	rows, err := s.Pool.Query(ctx, `SELECT ST_X(lonlat::geometry), ST_Y(lonlat::geometry), timestamp, coalesce(altitude_decimal, altitude),
		velocity, accuracy, battery, tracker_id, id FROM points WHERE user_id=$1 AND timestamp BETWEEN $2 AND $3 AND anomaly IS NOT TRUE
		AND lonlat IS NOT NULL ORDER BY timestamp`, userID, from, to)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var lon, lat float64
		var ts int32
		var alt *float64
		var vel, tracker *string
		var acc, bat *int32
		var id int64
		if err := rows.Scan(&lon, &lat, &ts, &alt, &vel, &acc, &bat, &tracker, &id); err != nil {
			return err
		}
		if err := fn(lon, lat, int64(ts), alt, vel, map[string]any{"id": id, "accuracy": acc, "battery": bat, "tracker_id": tracker}); err != nil {
			return err
		}
	}
	return rows.Err()
}
