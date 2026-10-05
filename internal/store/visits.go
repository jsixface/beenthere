package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

var VisitStatuses = []string{"suggested", "confirmed", "declined"}

type VisitOut struct {
	ID             int64      `json:"id"`
	AreaID         *int64     `json:"area_id"`
	UserID         int64      `json:"user_id"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        time.Time  `json:"ended_at"`
	Duration       int        `json:"duration"`
	Name           string     `json:"name"`
	Status         string     `json:"status"`
	Confidence     *int       `json:"confidence"`
	ConfidenceBand *string    `json:"confidence_band"`
	Place          VisitPlace `json:"place"`
}

type VisitPlace struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	ID        *int64   `json:"id"`
}

const visitSelect = `SELECT v.id, v.area_id, v.user_id, v.started_at, v.ended_at, v.duration, v.name, v.status,
	v.confidence, coalesce(ST_Y(p.lonlat::geometry), a.latitude::float8), coalesce(ST_X(p.lonlat::geometry), a.longitude::float8), p.id
	FROM visits v LEFT JOIN places p ON p.id = v.place_id LEFT JOIN areas a ON a.id = v.area_id`

func scanVisit(row interface{ Scan(...any) error }) (*VisitOut, error) {
	var v VisitOut
	var status int
	if err := row.Scan(&v.ID, &v.AreaID, &v.UserID, &v.StartedAt, &v.EndedAt, &v.Duration, &v.Name, &status,
		&v.Confidence, &v.Place.Latitude, &v.Place.Longitude, &v.Place.ID); err != nil {
		return nil, notFound(err)
	}
	if status >= 0 && status < len(VisitStatuses) {
		v.Status = VisitStatuses[status]
	}
	if v.Confidence != nil {
		b := "low"
		if *v.Confidence >= 70 {
			b = "high"
		} else if *v.Confidence >= 40 {
			b = "medium"
		}
		v.ConfidenceBand = &b
	}
	return &v, nil
}

type VisitFilter struct {
	UserID        int64
	From, To      time.Time
	Status        string
	BBox          *[4]float64 // sw_lng, sw_lat, ne_lng, ne_lat
	Page, PerPage int
}

func (f VisitFilter) where() (string, []any) {
	args := []any{f.UserID}
	w := "v.user_id = $1 AND v.deleted_at IS NULL AND v.status <> 2"
	if f.BBox != nil {
		args = append(args, f.BBox[0], f.BBox[1], f.BBox[2], f.BBox[3])
		w += fmt.Sprintf(` AND ((p.lonlat IS NOT NULL AND ST_Intersects(p.lonlat::geometry, ST_MakeEnvelope($2,$3,$4,$5,4326)))
			OR (p.lonlat IS NULL AND a.longitude BETWEEN $2 AND $4 AND a.latitude BETWEEN $3 AND $5))`)
	} else {
		args = append(args, f.From, f.To)
		w += " AND v.started_at BETWEEN $2 AND $3"
	}
	if f.Status != "" {
		for i, s := range VisitStatuses {
			if s == f.Status {
				args = append(args, i)
				w += fmt.Sprintf(" AND v.status = $%d", len(args))
			}
		}
	}
	return w, args
}

func (s *Store) ListVisits(ctx context.Context, f VisitFilter) ([]*VisitOut, int, error) {
	w, args := f.where()
	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM visits v LEFT JOIN places p ON p.id=v.place_id LEFT JOIN areas a ON a.id=v.area_id WHERE `+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := visitSelect + ` WHERE ` + w + ` ORDER BY v.started_at ASC`
	if f.PerPage > 0 {
		args = append(args, f.PerPage, (max(f.Page, 1)-1)*f.PerPage)
		q += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	}
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*VisitOut{}
	for rows.Next() {
		v, err := scanVisit(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

func (s *Store) GetVisit(ctx context.Context, userID, id int64) (*VisitOut, error) {
	return scanVisit(s.Pool.QueryRow(ctx, visitSelect+` WHERE v.user_id=$1 AND v.id=$2 AND v.deleted_at IS NULL AND v.status <> 2`, userID, id))
}

// CreateVisit creates a place (or reuses a nearby one) and the visit itself.
func (s *Store) CreateVisit(ctx context.Context, userID int64, name string, lat, lon float64, from, to time.Time, status string) (v *VisitOut, duplicate bool, err error) {
	st := 1
	if status == "suggested" {
		st = 0
	}
	var placeID int64
	err = s.Pool.QueryRow(ctx, `SELECT p.id FROM places p JOIN visits vv ON vv.place_id = p.id
		WHERE p.user_id=$1 AND vv.user_id=$1 AND ST_DWithin(p.lonlat, ST_SetSRID(ST_MakePoint($2,$3),4326)::geography, 100) LIMIT 1`,
		userID, lon, lat).Scan(&placeID)
	if err != nil {
		placeID, err = s.insertPlace(ctx, userID, name, lat, lon, 0, "")
		if err != nil {
			return nil, false, err
		}
	}
	if name == "" {
		_ = s.Pool.QueryRow(ctx, `SELECT name FROM places WHERE id=$1`, placeID).Scan(&name)
	}
	var id int64
	err = s.Pool.QueryRow(ctx, `INSERT INTO visits (user_id, name, place_id, started_at, ended_at, duration, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,now(),now()) ON CONFLICT (user_id, started_at, place_id) DO NOTHING RETURNING id`,
		userID, name, placeID, from, to, int(to.Sub(from).Minutes()), st).Scan(&id)
	if err != nil {
		// conflict: return the existing visit
		err = s.Pool.QueryRow(ctx, `SELECT id FROM visits WHERE user_id=$1 AND place_id=$2 AND started_at=$3`, userID, placeID, from).Scan(&id)
		if err != nil {
			return nil, false, err
		}
		duplicate = true
		_, _ = s.Pool.Exec(ctx, `UPDATE visits SET deleted_at=NULL, status=1 WHERE id=$1 AND (deleted_at IS NOT NULL OR status=2) AND $2`, id, st == 1)
	}
	v, err = s.GetVisit(ctx, userID, id)
	return v, duplicate, err
}

type VisitPatch struct {
	Name      *string
	PlaceID   *int64
	AreaID    *int64
	Status    *string
	StartedAt *time.Time
	EndedAt   *time.Time
}

func (s *Store) UpdateVisit(ctx context.Context, userID, id int64, p VisitPatch) (*VisitOut, error) {
	sets, args := []string{"updated_at = now()"}, []any{userID, id}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if p.Name != nil {
		add("name", *p.Name)
	}
	if p.PlaceID != nil {
		add("place_id", *p.PlaceID)
	}
	if p.AreaID != nil {
		add("area_id", *p.AreaID)
	}
	if p.Status != nil {
		for i, s := range VisitStatuses {
			if s == *p.Status {
				add("status", i)
			}
		}
	}
	if p.StartedAt != nil {
		add("started_at", *p.StartedAt)
	}
	if p.EndedAt != nil {
		add("ended_at", *p.EndedAt)
	}
	ct, err := s.Pool.Exec(ctx, `UPDATE visits SET `+strings.Join(sets, ", ")+` WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL`, args...)
	if err != nil {
		return nil, err
	}
	if ct.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	if p.StartedAt != nil || p.EndedAt != nil {
		_, _ = s.Pool.Exec(ctx, `UPDATE visits SET duration = (extract(epoch FROM ended_at - started_at)/60)::int WHERE id=$1`, id)
	}
	return s.GetVisit(ctx, userID, id)
}

// SoftDeleteVisit tombstones a visit so detection never resurrects it.
func (s *Store) SoftDeleteVisit(ctx context.Context, userID, id int64) error {
	ct, err := s.Pool.Exec(ctx, `UPDATE visits SET deleted_at=now(), updated_at=now() WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL`, userID, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) BulkUpdateVisitStatus(ctx context.Context, userID int64, ids []int64, status string) (int64, error) {
	for i, st := range VisitStatuses {
		if st == status {
			ct, err := s.Pool.Exec(ctx, `UPDATE visits SET status=$3, updated_at=now() WHERE user_id=$1 AND id = ANY($2) AND deleted_at IS NULL`, userID, ids, i)
			return ct.RowsAffected(), err
		}
	}
	return 0, fmt.Errorf("invalid status")
}

// MergeVisits merges visits (ordered by start) into the earliest one.
func (s *Store) MergeVisits(ctx context.Context, userID int64, ids []int64) (*VisitOut, error) {
	var keep int64
	err := s.Pool.QueryRow(ctx, `SELECT id FROM visits WHERE user_id=$1 AND id = ANY($2) AND deleted_at IS NULL ORDER BY started_at LIMIT 1`, userID, ids).Scan(&keep)
	if err != nil {
		return nil, notFound(err)
	}
	err = s.Tx(ctx, func(tx pgxTx) error {
		if _, err := tx.Exec(ctx, `UPDATE points SET visit_id=$3 WHERE user_id=$1 AND visit_id = ANY($2)`, userID, ids, keep); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE visits SET
			started_at=(SELECT min(started_at) FROM visits WHERE id=ANY($2)), ended_at=(SELECT max(ended_at) FROM visits WHERE id=ANY($2)),
			status=1, updated_at=now() WHERE id=$1`, keep, ids); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE visits SET duration=(extract(epoch FROM ended_at-started_at)/60)::int WHERE id=$1`, keep); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE visits SET deleted_at=now() WHERE user_id=$1 AND id = ANY($2) AND id <> $3`, userID, ids, keep)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetVisit(ctx, userID, keep)
}

// PersistStay stores a detected stay as a suggested visit, attributing it to a
// matching area or nearby place. It returns false when the stay was already
// known (or the user removed it).
func (s *Store) PersistStay(ctx context.Context, userID int64, lat, lon float64, from, to time.Time, pointIDs []int64) (bool, error) {
	// Don't overlap anything the user has confirmed/declined/deleted.
	var overlap bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM visits WHERE user_id=$1 AND started_at < $3 AND ended_at > $2
		AND (status <> 0 OR deleted_at IS NOT NULL))`, userID, from, to).Scan(&overlap); err != nil {
		return false, err
	}
	if overlap {
		return false, nil
	}
	var areaID *int64
	var areaName string
	var aid int64
	if err := s.Pool.QueryRow(ctx, `SELECT id, name FROM areas WHERE user_id=$1 AND
		ST_DWithin(ST_SetSRID(ST_MakePoint(longitude::float8, latitude::float8),4326)::geography, ST_SetSRID(ST_MakePoint($2,$3),4326)::geography, radius)
		ORDER BY radius LIMIT 1`, userID, lon, lat).Scan(&aid, &areaName); err == nil {
		areaID = &aid
	}
	var placeID int64
	name := areaName
	var pname string
	if err := s.Pool.QueryRow(ctx, `SELECT id, name FROM places WHERE user_id=$1 AND lonlat IS NOT NULL
		AND ST_DWithin(lonlat, ST_SetSRID(ST_MakePoint($2,$3),4326)::geography, 50) ORDER BY ST_Distance(lonlat, ST_SetSRID(ST_MakePoint($2,$3),4326)::geography) LIMIT 1`,
		userID, lon, lat).Scan(&placeID, &pname); err != nil {
		var err2 error
		placeID, err2 = s.insertPlace(ctx, userID, "", lat, lon, 1, "")
		if err2 != nil {
			return false, err2
		}
		pname = "Suggested place"
		_, _ = s.Pool.Exec(ctx, `UPDATE places SET name_locked_at=NULL WHERE id=$1`, placeID)
	}
	if name == "" {
		name = pname
	}
	// replace an existing machine-detected suggestion covering the same window
	_, _ = s.Pool.Exec(ctx, `UPDATE visits SET deleted_at = now() WHERE user_id=$1 AND status=0 AND deleted_at IS NULL AND import_id IS NULL
		AND started_at < $3 AND ended_at > $2`, userID, from, to)
	var id int64
	err := s.Pool.QueryRow(ctx, `INSERT INTO visits (user_id,name,place_id,area_id,started_at,ended_at,duration,status,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,0,now(),now()) ON CONFLICT (user_id, started_at, place_id) DO NOTHING RETURNING id`,
		userID, name, placeID, areaID, from, to, int(to.Sub(from).Minutes())).Scan(&id)
	if err != nil {
		return false, nil
	}
	if len(pointIDs) > 0 {
		_, err = s.Pool.Exec(ctx, `UPDATE points SET visit_id=$1 WHERE user_id=$2 AND id = ANY($3)`, id, userID, pointIDs)
	}
	return true, err
}
