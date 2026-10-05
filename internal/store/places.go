package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type TagOut struct {
	ID                  int64   `json:"id"`
	Name                string  `json:"name"`
	Icon                *string `json:"icon"`
	Color               *string `json:"color"`
	PrivacyRadiusMeters *int    `json:"privacy_radius_meters"`
}

type PlaceOut struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	Source      *int      `json:"source"`
	Note        *string   `json:"note"`
	Icon        *string   `json:"icon"`
	Color       *string   `json:"color"`
	VisitsCount int       `json:"visits_count"`
	NameLocked  bool      `json:"name_locked"`
	CreatedAt   time.Time `json:"created_at"`
	Tags        []TagOut  `json:"tags"`
}

const placeSelect = `SELECT p.id, p.name, coalesce(ST_Y(p.lonlat::geometry), p.latitude::float8), coalesce(ST_X(p.lonlat::geometry), p.longitude::float8),
	p.source, p.note, (SELECT count(*) FROM visits v WHERE v.place_id=p.id AND v.deleted_at IS NULL AND v.status<>2),
	p.name_locked_at IS NOT NULL, p.created_at FROM places p`

func (s *Store) loadPlaces(ctx context.Context, where string, args ...any) ([]PlaceOut, error) {
	rows, err := s.Pool.Query(ctx, placeSelect+" WHERE "+where, args...)
	if err != nil {
		return nil, err
	}
	out := []PlaceOut{}
	for rows.Next() {
		var p PlaceOut
		if err := rows.Scan(&p.ID, &p.Name, &p.Latitude, &p.Longitude, &p.Source, &p.Note, &p.VisitsCount, &p.NameLocked, &p.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		p.Tags = []TagOut{}
		out = append(out, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	ids := make([]int64, len(out))
	idx := map[int64]int{}
	for i, p := range out {
		ids[i], idx[p.ID] = p.ID, i
	}
	trows, err := s.Pool.Query(ctx, `SELECT tg.taggable_id, t.id, t.name, t.icon, t.color, t.privacy_radius_meters
		FROM taggings tg JOIN tags t ON t.id=tg.tag_id WHERE tg.taggable_type='Place' AND tg.taggable_id = ANY($1) ORDER BY tg.id`, ids)
	if err != nil {
		return nil, err
	}
	defer trows.Close()
	for trows.Next() {
		var pid int64
		var t TagOut
		if err := trows.Scan(&pid, &t.ID, &t.Name, &t.Icon, &t.Color, &t.PrivacyRadiusMeters); err != nil {
			return nil, err
		}
		i := idx[pid]
		out[i].Tags = append(out[i].Tags, t)
	}
	for i := range out {
		if len(out[i].Tags) > 0 {
			out[i].Icon, out[i].Color = out[i].Tags[0].Icon, out[i].Tags[0].Color
		}
	}
	return out, trows.Err()
}

type PlaceFilter struct {
	UserID        int64
	Filter        string
	TagIDs        []int64
	Untagged      bool
	Page, PerPage int
}

func (s *Store) ListPlaces(ctx context.Context, f PlaceFilter) ([]PlaceOut, int, error) {
	where, args := "p.user_id = $1", []any{f.UserID}
	tagged := `p.id IN (SELECT taggable_id FROM taggings WHERE taggable_type='Place')`
	confirmed := `p.id IN (SELECT place_id FROM visits WHERE user_id=$1 AND deleted_at IS NULL AND status=1 AND place_id IS NOT NULL)`
	switch f.Filter {
	case "all":
	case "manual":
		where += " AND p.source = 0"
	case "confirmed":
		where += " AND " + confirmed
	case "tagged":
		where += " AND " + tagged
	default:
		where += " AND (p.source IN (0,2) OR " + confirmed + " OR " + tagged + ")"
	}
	if len(f.TagIDs) > 0 || f.Untagged {
		var conds []string
		if len(f.TagIDs) > 0 {
			args = append(args, f.TagIDs)
			conds = append(conds, fmt.Sprintf(`p.id IN (SELECT taggable_id FROM taggings WHERE taggable_type='Place' AND tag_id = ANY($%d))`, len(args)))
		}
		if f.Untagged {
			conds = append(conds, `NOT `+tagged)
		}
		where += " AND (" + strings.Join(conds, " OR ") + ")"
	}
	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM places p WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	where += " ORDER BY p.id"
	if f.PerPage > 0 {
		args = append(args, f.PerPage, (max(f.Page, 1)-1)*f.PerPage)
		where += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	}
	out, err := s.loadPlaces(ctx, where, args...)
	return out, total, err
}

func (s *Store) GetPlace(ctx context.Context, userID, id int64) (*PlaceOut, error) {
	out, err := s.loadPlaces(ctx, "p.user_id=$1 AND p.id=$2", userID, id)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return &out[0], nil
}

func (s *Store) insertPlace(ctx context.Context, userID int64, name string, lat, lon float64, source int, note string) (int64, error) {
	if name == "" {
		name = "Suggested place"
	}
	var n any
	if note != "" {
		n = note
	}
	var id int64
	err := s.Pool.QueryRow(ctx, `INSERT INTO places (user_id,name,latitude,longitude,lonlat,source,note,geodata,created_at,updated_at,name_locked_at)
		VALUES ($1,$2,$3::float8,$4::float8,ST_SetSRID(ST_MakePoint($4::float8,$3::float8),4326)::geography,$5::int,$6,'{}'::jsonb,now(),now(),CASE WHEN $5::int=0 THEN now() END) RETURNING id`,
		userID, name, lat, lon, source, n).Scan(&id)
	return id, err
}

func (s *Store) CreatePlace(ctx context.Context, userID int64, name string, lat, lon float64, source int, note string, tagIDs []int64) (*PlaceOut, error) {
	id, err := s.insertPlace(ctx, userID, name, lat, lon, source, note)
	if err != nil {
		return nil, err
	}
	if err := s.SetPlaceTags(ctx, userID, id, tagIDs); err != nil {
		return nil, err
	}
	return s.GetPlace(ctx, userID, id)
}

type PlacePatch struct {
	Name     *string
	Lat, Lon *float64
	Note     *string
	TagIDs   *[]int64
}

func (s *Store) UpdatePlace(ctx context.Context, userID, id int64, p PlacePatch) (*PlaceOut, error) {
	cur, err := s.GetPlace(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	name, lat, lon, note := cur.Name, cur.Latitude, cur.Longitude, ""
	if cur.Note != nil {
		note = *cur.Note
	}
	locked := false
	if p.Name != nil && *p.Name != cur.Name {
		name, locked = *p.Name, true
	}
	if p.Lat != nil {
		lat = *p.Lat
	}
	if p.Lon != nil {
		lon = *p.Lon
	}
	if p.Note != nil {
		note = *p.Note
	}
	_, err = s.Pool.Exec(ctx, `UPDATE places SET name=$3, latitude=$4::float8, longitude=$5::float8, lonlat=ST_SetSRID(ST_MakePoint($5::float8,$4::float8),4326)::geography,
		note=nullif($6,''), name_locked_at = CASE WHEN $7 THEN now() ELSE name_locked_at END, updated_at=now() WHERE user_id=$1 AND id=$2`,
		userID, id, name, lat, lon, note, locked)
	if err != nil {
		return nil, err
	}
	if p.TagIDs != nil {
		if err := s.SetPlaceTags(ctx, userID, id, *p.TagIDs); err != nil {
			return nil, err
		}
	}
	return s.GetPlace(ctx, userID, id)
}

func (s *Store) DeletePlace(ctx context.Context, userID, id int64) error {
	return s.Tx(ctx, func(tx pgxTx) error {
		if _, err := tx.Exec(ctx, `UPDATE visits SET place_id=NULL WHERE place_id=$1 AND user_id=$2`, id, userID); err != nil {
			return err
		}
		_, _ = tx.Exec(ctx, `DELETE FROM place_visits WHERE place_id=$1`, id)
		_, _ = tx.Exec(ctx, `DELETE FROM taggings WHERE taggable_type='Place' AND taggable_id=$1`, id)
		ct, err := tx.Exec(ctx, `DELETE FROM places WHERE id=$1 AND user_id=$2`, id, userID)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (s *Store) SetPlaceTags(ctx context.Context, userID, placeID int64, tagIDs []int64) error {
	return s.Tx(ctx, func(tx pgxTx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM taggings WHERE taggable_type='Place' AND taggable_id=$1`, placeID); err != nil {
			return err
		}
		if len(tagIDs) == 0 {
			return nil
		}
		_, err := tx.Exec(ctx, `INSERT INTO taggings (tag_id, taggable_type, taggable_id, created_at, updated_at)
			SELECT id,'Place',$2,now(),now() FROM tags WHERE user_id=$1 AND id = ANY($3) ON CONFLICT DO NOTHING`, userID, placeID, tagIDs)
		return err
	})
}

// NearbyPlaces finds the user's places within radius km.
func (s *Store) NearbyPlaces(ctx context.Context, userID int64, lat, lon, radiusKm float64, limit int, q string) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, name, ST_Y(lonlat::geometry), ST_X(lonlat::geometry),
		ST_Distance(lonlat, ST_SetSRID(ST_MakePoint($3,$2),4326)::geography) AS d FROM places
		WHERE user_id=$1 AND lonlat IS NOT NULL AND ST_DWithin(lonlat, ST_SetSRID(ST_MakePoint($3,$2),4326)::geography, $4)
		AND ($6 = '' OR name ILIKE '%'||$6||'%') ORDER BY d LIMIT $5`, userID, lat, lon, radiusKm*1000, limit, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var name string
		var la, lo, d float64
		if err := rows.Scan(&id, &name, &la, &lo, &d); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "name": name, "latitude": la, "longitude": lo, "distance": d / 1000, "source": "user"})
	}
	return out, rows.Err()
}

// ---- tags ----

func (s *Store) PrivacyZones(ctx context.Context, userID int64) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `SELECT t.id, t.name, t.icon, t.color, t.privacy_radius_meters FROM tags t
		WHERE t.user_id=$1 AND t.privacy_radius_meters IS NOT NULL ORDER BY t.name`, userID)
	if err != nil {
		return nil, err
	}
	type tg struct {
		id     int64
		name   string
		icon   *string
		color  *string
		radius *int
	}
	var tags []tg
	for rows.Next() {
		var t tg
		if err := rows.Scan(&t.id, &t.name, &t.icon, &t.color, &t.radius); err != nil {
			rows.Close()
			return nil, err
		}
		tags = append(tags, t)
	}
	rows.Close()
	out := []map[string]any{}
	for _, t := range tags {
		prows, err := s.Pool.Query(ctx, `SELECT p.id, p.name, coalesce(ST_Y(p.lonlat::geometry), p.latitude::float8), coalesce(ST_X(p.lonlat::geometry), p.longitude::float8)
			FROM places p JOIN taggings tg ON tg.taggable_id=p.id AND tg.taggable_type='Place' WHERE tg.tag_id=$1`, t.id)
		if err != nil {
			return nil, err
		}
		places := []map[string]any{}
		for prows.Next() {
			var id int64
			var name string
			var la, lo float64
			if err := prows.Scan(&id, &name, &la, &lo); err != nil {
				prows.Close()
				return nil, err
			}
			places = append(places, map[string]any{"id": id, "name": name, "latitude": la, "longitude": lo})
		}
		prows.Close()
		out = append(out, map[string]any{"tag_id": t.id, "tag_name": t.name, "tag_icon": t.icon, "tag_color": t.color,
			"radius_meters": t.radius, "places": places})
	}
	return out, nil
}

func (s *Store) ListTags(ctx context.Context, userID int64) ([]TagOut, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id,name,icon,color,privacy_radius_meters FROM tags WHERE user_id=$1 ORDER BY name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TagOut{}
	for rows.Next() {
		var t TagOut
		if err := rows.Scan(&t.ID, &t.Name, &t.Icon, &t.Color, &t.PrivacyRadiusMeters); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) UpsertTag(ctx context.Context, userID int64, id int64, name string, icon, color *string, radius *int) (*TagOut, error) {
	var t TagOut
	var err error
	if id == 0 {
		err = s.Pool.QueryRow(ctx, `INSERT INTO tags (user_id,name,icon,color,privacy_radius_meters,created_at,updated_at)
			VALUES ($1,$2,$3,$4,$5,now(),now()) RETURNING id,name,icon,color,privacy_radius_meters`, userID, name, icon, color, radius).
			Scan(&t.ID, &t.Name, &t.Icon, &t.Color, &t.PrivacyRadiusMeters)
	} else {
		err = s.Pool.QueryRow(ctx, `UPDATE tags SET name=$3,icon=$4,color=$5,privacy_radius_meters=$6,updated_at=now()
			WHERE user_id=$1 AND id=$2 RETURNING id,name,icon,color,privacy_radius_meters`, userID, id, name, icon, color, radius).
			Scan(&t.ID, &t.Name, &t.Icon, &t.Color, &t.PrivacyRadiusMeters)
	}
	return &t, notFound(err)
}

func (s *Store) DeleteTag(ctx context.Context, userID, id int64) error {
	return s.Tx(ctx, func(tx pgxTx) error {
		_, _ = tx.Exec(ctx, `DELETE FROM taggings WHERE tag_id=$1 AND tag_id IN (SELECT id FROM tags WHERE user_id=$2)`, id, userID)
		ct, err := tx.Exec(ctx, `DELETE FROM tags WHERE id=$1 AND user_id=$2`, id, userID)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ---- notes ----

type NoteOut struct {
	ID             int64      `json:"id"`
	Title          *string    `json:"title"`
	Body           string     `json:"body"`
	Latitude       *float64   `json:"latitude"`
	Longitude      *float64   `json:"longitude"`
	AttachableType *string    `json:"attachable_type"`
	AttachableID   *int64     `json:"attachable_id"`
	Date           *string    `json:"date"`
	NotedAt        *time.Time `json:"noted_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

const noteSelect = `SELECT id,title,body,ST_Y(lonlat::geometry),ST_X(lonlat::geometry),attachable_type,attachable_id,
	to_char(noted_at AT TIME ZONE 'UTC','YYYY-MM-DD'),noted_at,created_at,updated_at FROM notes`

func scanNote(row interface{ Scan(...any) error }) (*NoteOut, error) {
	var n NoteOut
	var body *string
	if err := row.Scan(&n.ID, &n.Title, &body, &n.Latitude, &n.Longitude, &n.AttachableType, &n.AttachableID, &n.Date, &n.NotedAt, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	if body != nil {
		n.Body = *body
	}
	return &n, nil
}

func (s *Store) ListNotes(ctx context.Context, userID int64, atype string, aid *int64, standalone bool) ([]*NoteOut, error) {
	w, args := "user_id=$1", []any{userID}
	if atype != "" {
		args = append(args, atype)
		w += fmt.Sprintf(" AND attachable_type=$%d", len(args))
	}
	if aid != nil {
		args = append(args, *aid)
		w += fmt.Sprintf(" AND attachable_id=$%d", len(args))
	}
	if standalone {
		w += " AND attachable_id IS NULL"
	}
	rows, err := s.Pool.Query(ctx, noteSelect+" WHERE "+w+" ORDER BY noted_at DESC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*NoteOut{}
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) GetNote(ctx context.Context, userID, id int64) (*NoteOut, error) {
	return scanNote(s.Pool.QueryRow(ctx, noteSelect+" WHERE user_id=$1 AND id=$2", userID, id))
}

type NoteIn struct {
	Title          *string
	Body           *string
	Lat, Lon       *float64
	AttachableType *string
	AttachableID   *int64
	NotedAt        *time.Time
}

func (s *Store) CreateNote(ctx context.Context, userID int64, n NoteIn) (*NoteOut, error) {
	if n.NotedAt == nil {
		now := time.Now()
		n.NotedAt = &now
	}
	var id int64
	err := s.Pool.QueryRow(ctx, `INSERT INTO notes (user_id,title,body,lonlat,attachable_type,attachable_id,noted_at,created_at,updated_at)
		VALUES ($1,$2,$3,CASE WHEN $4::float8 IS NULL THEN NULL ELSE ST_SetSRID(ST_MakePoint($4,$5),4326)::geography END,$6,$7,$8,now(),now()) RETURNING id`,
		userID, n.Title, n.Body, n.Lon, n.Lat, n.AttachableType, n.AttachableID, n.NotedAt).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetNote(ctx, userID, id)
}

func (s *Store) UpdateNote(ctx context.Context, userID, id int64, n NoteIn) (*NoteOut, error) {
	ct, err := s.Pool.Exec(ctx, `UPDATE notes SET title=coalesce($3,title), body=coalesce($4,body),
		lonlat=CASE WHEN $5::float8 IS NULL THEN lonlat ELSE ST_SetSRID(ST_MakePoint($5,$6),4326)::geography END,
		noted_at=coalesce($7,noted_at), updated_at=now() WHERE user_id=$1 AND id=$2`, userID, id, n.Title, n.Body, n.Lon, n.Lat, n.NotedAt)
	if err != nil {
		return nil, err
	}
	if ct.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.GetNote(ctx, userID, id)
}

func (s *Store) DeleteNote(ctx context.Context, userID, id int64) error {
	ct, err := s.Pool.Exec(ctx, `DELETE FROM notes WHERE user_id=$1 AND id=$2`, userID, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

var _ = json.Marshal
