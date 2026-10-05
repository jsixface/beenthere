package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Enum label tables mirror Point's Rails enums.
var (
	BatteryStatuses = []string{"unknown", "unplugged", "charging", "full", "connected_not_charging", "discharging"}
	Triggers        = []string{"unknown", "background_event", "circular_region_event", "beacon_event",
		"report_location_message_event", "manual_event", "timer_based_event", "settings_monitoring_event"}
	Connections = map[int]string{0: "mobile", 1: "wifi", 2: "offline", 4: "unknown"}
)

func enumIndex(list []string, v string) *int32 {
	for i, s := range list {
		if s == v {
			n := int32(i)
			return &n
		}
	}
	return nil
}

func BatteryStatusCode(v string) *int32 { return enumIndex(BatteryStatuses, v) }
func TriggerCode(v string) *int32       { return enumIndex(Triggers, v) }
func ConnectionCode(v string) *int32 {
	for k, s := range Connections {
		if s == v {
			n := int32(k)
			return &n
		}
	}
	return nil
}

// PointIn is a point ready for insertion. Nil pointers become NULL.
type PointIn struct {
	Lon, Lat         float64
	Timestamp        int64
	Battery          *int32
	BatteryStatus    *int32
	Altitude         *float64
	Accuracy         *int32
	VerticalAccuracy *int32
	Velocity         *string
	TrackerID        *string
	SSID             *string
	BSSID            *string
	Ping             *string
	Topic            *string
	Connection       *int32
	Trigger          *int32
	InRIDs           []string
	InRegions        []string
	Course           *float64
	CourseAccuracy   *float64
	MotionData       map[string]any
	RawData          any
	ImportID         *int64
	TrackID          *int64
	PointID          *int64
}

type dedupKey struct {
	lon, lat float64
	ts       int64
}

// UpsertResult describes the rows written by UpsertPoints.
type UpsertResult struct {
	IDs        []int64
	Timestamps []int64
	Lons, Lats []float64
	Inserted   int
}

const upsertChunk = 2000

// UpsertPoints inserts points for a user, updating rows that collide on
// (user_id, timestamp, lonlat) exactly like the Rails ingest path, and keeps
// users.points_count in step.
func (s *Store) UpsertPoints(ctx context.Context, userID int64, in []PointIn) (*UpsertResult, error) {
	res := &UpsertResult{}
	seen := make(map[dedupKey]struct{}, len(in))
	uniq := in[:0:0]
	for _, p := range in {
		if math.IsNaN(p.Lon) || math.IsNaN(p.Lat) {
			continue
		}
		k := dedupKey{p.Lon, p.Lat, p.Timestamp}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		uniq = append(uniq, p)
	}
	for start := 0; start < len(uniq); start += upsertChunk {
		end := min(start+upsertChunk, len(uniq))
		if err := s.upsertChunk(ctx, userID, uniq[start:end], res); err != nil {
			return res, err
		}
	}
	if res.Inserted > 0 {
		if _, err := s.Pool.Exec(ctx, `UPDATE users SET points_count = points_count + $2 WHERE id = $1`,
			userID, res.Inserted); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (s *Store) upsertChunk(ctx context.Context, userID int64, ps []PointIn, res *UpsertResult) error {
	n := len(ps)
	lon, lat := make([]float64, n), make([]float64, n)
	ts := make([]int32, n)
	battery, bstatus, acc, vacc, conn, trig := make([]*int32, n), make([]*int32, n), make([]*int32, n), make([]*int32, n), make([]*int32, n), make([]*int32, n)
	alt, course, cacc := make([]*float64, n), make([]*float64, n), make([]*float64, n)
	altInt := make([]*int32, n)
	vel, tracker, ssid, bssid, ping, topic := make([]*string, n), make([]*string, n), make([]*string, n), make([]*string, n), make([]*string, n), make([]*string, n)
	inrids, inregions := make([]string, n), make([]string, n)
	motion, raw := make([]string, n), make([]string, n)
	importID := make([]*int64, n)
	for i, p := range ps {
		lon[i], lat[i], ts[i] = p.Lon, p.Lat, int32(p.Timestamp)
		battery[i], bstatus[i], acc[i], vacc[i], conn[i], trig[i] = p.Battery, p.BatteryStatus, p.Accuracy, p.VerticalAccuracy, p.Connection, p.Trigger
		alt[i], course[i], cacc[i] = p.Altitude, p.Course, p.CourseAccuracy
		if p.Altitude != nil {
			v := int32(math.Round(*p.Altitude))
			altInt[i] = &v
		}
		vel[i], tracker[i], ssid[i], bssid[i], ping[i], topic[i] = p.Velocity, p.TrackerID, p.SSID, p.BSSID, p.Ping, p.Topic
		inrids[i], inregions[i] = pgTextArray(p.InRIDs), pgTextArray(p.InRegions)
		md := p.MotionData
		if md == nil {
			md = map[string]any{}
		}
		b, _ := json.Marshal(md)
		motion[i] = string(b)
		rb, _ := json.Marshal(p.RawData)
		if p.RawData == nil {
			rb = []byte("{}")
		}
		raw[i] = string(rb)
		importID[i] = p.ImportID
	}
	const q = `
INSERT INTO points (user_id, lonlat, timestamp, battery, battery_status, altitude, altitude_decimal, accuracy,
  vertical_accuracy, velocity, tracker_id, ssid, bssid, ping, topic, connection, trigger, inrids, in_regions,
  course, course_accuracy, motion_data, raw_data, import_id, geodata, created_at, updated_at)
SELECT $1, ST_SetSRID(ST_MakePoint(t.lon, t.lat), 4326)::geography, t.ts, t.battery, t.bstatus, t.alt_int, t.alt,
  t.acc, t.vacc, t.vel, t.tracker, t.ssid, t.bssid, t.ping, t.topic, t.conn, t.trig,
  t.inrids::text[], t.inregions::text[], t.course, t.cacc, t.motion::jsonb, t.raw::jsonb, t.import_id,
  '{}'::jsonb, now(), now()
FROM unnest($2::float8[], $3::float8[], $4::int4[], $5::int4[], $6::int4[], $7::int4[], $8::int4[],
  $9::int4[], $10::int4[], $11::text[], $12::text[], $13::text[], $14::text[], $15::text[], $16::text[],
  $17::int4[], $18::numeric[], $19::text[], $20::text[], $21::numeric[], $22::numeric[], $23::text[], $24::text[], $25::int8[])
  AS t(lon, lat, ts, battery, bstatus, acc, vacc, conn, trig, vel, tracker, ssid, bssid, ping, topic, alt_int, alt,
       inrids, inregions, course, cacc, motion, raw, import_id)
ORDER BY t.lon, t.lat, t.ts
ON CONFLICT (user_id, timestamp, lonlat) DO UPDATE SET
  battery = excluded.battery, battery_status = excluded.battery_status, altitude = excluded.altitude,
  altitude_decimal = excluded.altitude_decimal, accuracy = excluded.accuracy,
  vertical_accuracy = excluded.vertical_accuracy, velocity = excluded.velocity, tracker_id = excluded.tracker_id,
  ssid = excluded.ssid, bssid = excluded.bssid, ping = excluded.ping, topic = excluded.topic,
  connection = excluded.connection, trigger = excluded.trigger, inrids = excluded.inrids,
  in_regions = excluded.in_regions, course = excluded.course, course_accuracy = excluded.course_accuracy,
  motion_data = excluded.motion_data,
  raw_data_archived = CASE WHEN points.raw_data IS DISTINCT FROM excluded.raw_data THEN FALSE ELSE points.raw_data_archived END,
  raw_data_archive_id = CASE WHEN points.raw_data IS DISTINCT FROM excluded.raw_data THEN NULL ELSE points.raw_data_archive_id END,
  raw_data = excluded.raw_data, updated_at = now()
RETURNING id, timestamp, ST_X(lonlat::geometry), ST_Y(lonlat::geometry), (xmax = 0)`
	rows, err := s.Pool.Query(ctx, q, userID, lon, lat, ts, battery, bstatus, acc, vacc, conn, trig,
		vel, tracker, ssid, bssid, ping, topic, altInt, alt, inrids, inregions, course, cacc, motion, raw, importID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var t int32
		var x, y float64
		var inserted bool
		if err := rows.Scan(&id, &t, &x, &y, &inserted); err != nil {
			return err
		}
		res.IDs = append(res.IDs, id)
		res.Timestamps = append(res.Timestamps, int64(t))
		res.Lons, res.Lats = append(res.Lons, x), append(res.Lats, y)
		if inserted {
			res.Inserted++
		}
	}
	return rows.Err()
}

// pgTextArray renders a Postgres array literal, for passing text[] through unnest.
func pgTextArray(in []string) string {
	if len(in) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, s := range in {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		b.WriteString(strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

// PointOut is the API representation of a stored point (Api::PointSerializer).
type PointOut struct {
	ID               int64           `json:"id"`
	Accuracy         *int32          `json:"accuracy"`
	Altitude         *float64        `json:"altitude"`
	AltitudeDecimal  *float64        `json:"altitude_decimal"`
	Anomaly          *bool           `json:"anomaly"`
	Battery          *int32          `json:"battery"`
	BatteryStatus    *string         `json:"battery_status"`
	BSSID            *string         `json:"bssid"`
	City             *string         `json:"city"`
	Connection       *string         `json:"connection"`
	Country          *string         `json:"country"`
	CountryName      string          `json:"country_name"`
	Course           *float64        `json:"course"`
	CourseAccuracy   *float64        `json:"course_accuracy"`
	ExternalTrackID  *string         `json:"external_track_id"`
	Geodata          json.RawMessage `json:"geodata"`
	InRegions        []string        `json:"in_regions"`
	InRIDs           []string        `json:"inrids"`
	Mode             *int32          `json:"mode"`
	MotionData       json.RawMessage `json:"motion_data"`
	Ping             *string         `json:"ping"`
	ReverseGeocoded  *time.Time      `json:"reverse_geocoded_at"`
	SSID             *string         `json:"ssid"`
	Timestamp        *int64          `json:"timestamp"`
	Topic            *string         `json:"topic"`
	TrackID          *int64          `json:"track_id"`
	TrackerID        *string         `json:"tracker_id"`
	Trigger          *string         `json:"trigger"`
	Velocity         *string         `json:"velocity"`
	VerticalAccuracy *int32          `json:"vertical_accuracy"`
	Latitude         string          `json:"latitude"`
	Longitude        string          `json:"longitude"`
	Revision         int32           `json:"revision"`
}

// SlimPoint is Api::SlimPointSerializer.
type SlimPoint struct {
	ID          int64   `json:"id"`
	Latitude    string  `json:"latitude"`
	Longitude   string  `json:"longitude"`
	Timestamp   int64   `json:"timestamp"`
	Velocity    *string `json:"velocity"`
	CountryName string  `json:"country_name"`
	TrackerID   *string `json:"tracker_id"`
}

type PointFilter struct {
	UserID           int64
	StartAt, EndAt   int64
	ImportID         *int64
	TrackID          *int64
	PointID          *int64
	AnomaliesOnly    bool
	IncludeAnomalies bool
	BBox             *[4]float64 // minLon, minLat, maxLon, maxLat
	Desc             bool
	Page, PerPage    int
}

func (f PointFilter) where() (string, []any) {
	args := []any{f.UserID, f.StartAt, f.EndAt}
	w := "p.user_id = $1 AND p.timestamp BETWEEN $2 AND $3"
	switch {
	case f.AnomaliesOnly:
		w += " AND p.anomaly IS TRUE"
	case f.IncludeAnomalies:
	default:
		w += " AND p.anomaly IS NOT TRUE"
	}
	if f.ImportID != nil {
		args = append(args, *f.ImportID)
		w += fmt.Sprintf(" AND p.import_id = $%d", len(args))
	}
	if f.PointID != nil {
		args = append(args, *f.PointID)
		w += fmt.Sprintf(" AND p.id = $%d", len(args))
	}
	if f.TrackID != nil {
		args = append(args, *f.TrackID)
		w += fmt.Sprintf(" AND p.track_id = $%d", len(args))
	}
	if f.BBox != nil {
		args = append(args, f.BBox[0], f.BBox[1], f.BBox[2], f.BBox[3])
		n := len(args)
		env := fmt.Sprintf("ST_MakeEnvelope($%d,$%d,$%d,$%d,4326)", n-3, n-2, n-1, n)
		w += " AND p.lonlat && " + env + "::geography AND ST_Intersects(p.lonlat::geometry, " + env + ")"
	}
	return w, args
}

// CountPoints returns count, max timestamp and max updated_at for ETag use.
func (s *Store) CountPoints(ctx context.Context, f PointFilter) (count int64, maxTS *int64, maxUpdated *time.Time, err error) {
	w, args := f.where()
	err = s.Pool.QueryRow(ctx,
		`SELECT count(*), max(p.timestamp), max(p.updated_at) FROM points p WHERE `+w, args...).Scan(&count, &maxTS, &maxUpdated)
	return
}

func orderDir(desc bool) string {
	if desc {
		return "DESC"
	}
	return "ASC"
}

const pointCols = `p.id, p.accuracy, p.altitude, p.altitude_decimal, p.anomaly, p.battery, p.battery_status,
 p.bssid, p.city, p.connection, p.country, coalesce(p.country_name, c.name, p.country, ''), p.course, p.course_accuracy,
 p.external_track_id, coalesce(p.geodata,'{}'::jsonb), coalesce(p.in_regions,'{}'), coalesce(p.inrids,'{}'),
 p.mode, coalesce(p.motion_data,'{}'::jsonb), p.ping, p.reverse_geocoded_at, p.ssid, p.timestamp, p.topic,
 p.track_id, p.tracker_id, p.trigger, p.velocity, p.vertical_accuracy,
 ST_Y(p.lonlat::geometry), ST_X(p.lonlat::geometry), p.lock_version`

func (s *Store) ListPoints(ctx context.Context, f PointFilter) ([]PointOut, error) {
	w, args := f.where()
	args = append(args, f.PerPage, (f.Page-1)*f.PerPage)
	q := fmt.Sprintf(`SELECT %s FROM points p LEFT JOIN countries c ON c.id = p.country_id WHERE %s
		ORDER BY p.timestamp %s LIMIT $%d OFFSET $%d`, pointCols, w, orderDir(f.Desc), len(args)-1, len(args))
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PointOut{}
	for rows.Next() {
		var p PointOut
		var alt *float64
		var bs, tr, cn *int32
		var lat, lon float64
		if err := rows.Scan(&p.ID, &p.Accuracy, &alt, &p.AltitudeDecimal, &p.Anomaly, &p.Battery, &bs,
			&p.BSSID, &p.City, &cn, &p.Country, &p.CountryName, &p.Course, &p.CourseAccuracy,
			&p.ExternalTrackID, &p.Geodata, &p.InRegions, &p.InRIDs,
			&p.Mode, &p.MotionData, &p.Ping, &p.ReverseGeocoded, &p.SSID, &p.Timestamp, &p.Topic,
			&p.TrackID, &p.TrackerID, &tr, &p.Velocity, &p.VerticalAccuracy, &lat, &lon, &p.Revision); err != nil {
			return nil, err
		}
		p.Altitude = alt
		if p.AltitudeDecimal != nil {
			p.Altitude = p.AltitudeDecimal
		}
		p.BatteryStatus = labelOf(bs, func(i int) string {
			if i >= 0 && i < len(BatteryStatuses) {
				return BatteryStatuses[i]
			}
			return ""
		})
		p.Trigger = labelOf(tr, func(i int) string {
			if i >= 0 && i < len(Triggers) {
				return Triggers[i]
			}
			return ""
		})
		p.Connection = labelOf(cn, func(i int) string { return Connections[i] })
		p.Latitude, p.Longitude = fmt.Sprint(lat), fmt.Sprint(lon)
		out = append(out, p)
	}
	return out, rows.Err()
}

func labelOf(v *int32, f func(int) string) *string {
	if v == nil {
		return nil
	}
	s := f(int(*v))
	if s == "" {
		return nil
	}
	return &s
}

func (s *Store) ListSlimPoints(ctx context.Context, f PointFilter) ([]SlimPoint, error) {
	w, args := f.where()
	args = append(args, f.PerPage, (f.Page-1)*f.PerPage)
	q := fmt.Sprintf(`SELECT p.id, ST_Y(p.lonlat::geometry), ST_X(p.lonlat::geometry), p.timestamp, p.velocity,
		coalesce(p.country_name, c.name, p.country, ''), p.tracker_id
		FROM points p LEFT JOIN countries c ON c.id = p.country_id WHERE %s
		ORDER BY p.timestamp %s LIMIT $%d OFFSET $%d`, w, orderDir(f.Desc), len(args)-1, len(args))
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SlimPoint{}
	for rows.Next() {
		var p SlimPoint
		var lat, lon float64
		if err := rows.Scan(&p.ID, &lat, &lon, &p.Timestamp, &p.Velocity, &p.CountryName, &p.TrackerID); err != nil {
			return nil, err
		}
		p.Latitude, p.Longitude = fmt.Sprint(lat), fmt.Sprint(lon)
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeletePoints removes points owned by the user and fixes points_count.
func (s *Store) DeletePoints(ctx context.Context, userID int64, ids []int64) (int64, error) {
	var n int64
	err := s.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`DELETE FROM points WHERE user_id = $1 AND id = ANY($2) RETURNING track_id, import_id`, userID, ids)
		if err != nil {
			return err
		}
		tracks := map[int64]struct{}{}
		for rows.Next() {
			var t, im *int64
			if err := rows.Scan(&t, &im); err != nil {
				rows.Close()
				return err
			}
			n++
			if t != nil {
				tracks[*t] = struct{}{}
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if n > 0 {
			if _, err := tx.Exec(ctx, `UPDATE users SET points_count = greatest(points_count - $2, 0) WHERE id = $1`, userID, n); err != nil {
				return err
			}
		}
		for t := range tracks {
			if err := RefreshTrackFromPoints(ctx, tx, userID, t); err != nil {
				return err
			}
		}
		return nil
	})
	return n, err
}

// MovePoint relocates a point (PATCH /points/:id).
func (s *Store) MovePoint(ctx context.Context, userID, id int64, lon, lat float64) (*int64, error) {
	var track *int64
	err := s.Pool.QueryRow(ctx, `UPDATE points SET lonlat = ST_SetSRID(ST_MakePoint($3,$4),4326)::geography,
		country_id = (SELECT id FROM countries WHERE ST_Contains(geom, ST_SetSRID(ST_MakePoint($3,$4),4326)) LIMIT 1),
		city = NULL, geodata = '{}', reverse_geocoded_at = NULL, lock_version = lock_version + 1, updated_at = now()
		WHERE id = $1 AND user_id = $2 RETURNING track_id`, id, userID, lon, lat).Scan(&track)
	return track, notFound(err)
}

// TrackedMonths returns {year: [month names]} (Api::V1::Points::TrackedMonths).
func (s *Store) TrackedMonths(ctx context.Context, userID int64, tz string) (map[int][]int, error) {
	rows, err := s.Pool.Query(ctx, `SELECT DISTINCT extract(year FROM to_timestamp(timestamp) AT TIME ZONE $2)::int,
		extract(month FROM to_timestamp(timestamp) AT TIME ZONE $2)::int FROM points WHERE user_id = $1 ORDER BY 1, 2`, userID, tz)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int][]int{}
	for rows.Next() {
		var y, m int
		if err := rows.Scan(&y, &m); err != nil {
			return nil, err
		}
		out[y] = append(out[y], m)
	}
	return out, rows.Err()
}

// AssignCountries fills country_id/country_name for freshly ingested points
// using the countries table (the Rails app does this in an after_create hook).
func (s *Store) AssignCountries(ctx context.Context, userID int64, ids []int64) error {
	_, err := s.Pool.Exec(ctx, `UPDATE points p SET country_id = c.id, country_name = c.name, country = c.name
		FROM countries c WHERE p.id = ANY($1) AND p.user_id = $2 AND p.country_id IS NULL
		AND ST_Contains(c.geom, p.lonlat::geometry)`, ids, userID)
	return err
}
