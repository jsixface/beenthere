// Package stats computes the monthly aggregates stored in the stats table.
package stats

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/sixface/beenthere/internal/store"
)

const (
	CalculationVersion  = 3
	flyoverKmh          = 500.0
	bridgeCapSeconds    = 3600 // Visits::Detection::Policy::BRIDGE_CAP_S upper bound used for runs
	minTransitRunPoints = 2
)

// GeoPoint is a reverse-geocoded point used for toponym computation.
type GeoPoint struct {
	ID        int64
	Timestamp int64
	City      *string
	Country   *string
	Velocity  float64 // m/s
}

type City struct {
	City      string `json:"city"`
	Points    int    `json:"points"`
	Timestamp int64  `json:"timestamp"`
	StayedFor int64  `json:"stayed_for"`
}

type Toponym struct {
	Country string `json:"country"`
	Cities  []City `json:"cities"`
}

type run struct {
	country, city string
	first, last   int64
	points        int
}

// Toponyms groups geocoded points into per-country/city presence runs and
// keeps cities where the user stayed at least minMinutes (Stats::Toponyms).
func Toponyms(pts []GeoPoint, minMinutes int64) []Toponym {
	sort.Slice(pts, func(i, j int) bool {
		if pts[i].Timestamp != pts[j].Timestamp {
			return pts[i].Timestamp < pts[j].Timestamp
		}
		return pts[i].ID < pts[j].ID
	})
	type totals struct {
		seconds   int64
		points    int
		timestamp int64
	}
	countries := map[string]map[string]*totals{}
	var order []string
	var cur *run
	flight := 0
	finish := func() {
		if cur == nil {
			return
		}
		cm, ok := countries[cur.country]
		if !ok {
			cm = map[string]*totals{}
			countries[cur.country] = cm
			order = append(order, cur.country)
		}
		t, ok := cm[cur.city]
		if !ok {
			t = &totals{}
			cm[cur.city] = t
		}
		t.seconds += cur.last - cur.first
		t.points += cur.points
		t.timestamp = cur.last
		cur = nil
	}
	for _, p := range pts {
		if p.Velocity*3.6 > flyoverKmh {
			flight++
			if flight == minTransitRunPoints {
				finish()
			}
			continue
		}
		flight = 0
		if p.Country == nil || p.City == nil || *p.Country == "" || *p.City == "" {
			continue
		}
		if cur != nil && (cur.country != *p.Country || cur.city != *p.City || p.Timestamp-cur.last > bridgeCapSeconds) {
			finish()
		}
		if cur == nil {
			cur = &run{country: *p.Country, city: *p.City, first: p.Timestamp, last: p.Timestamp}
		}
		cur.last = p.Timestamp
		cur.points++
	}
	finish()
	out := make([]Toponym, 0, len(order))
	for _, c := range order {
		t := Toponym{Country: c, Cities: []City{}}
		names := make([]string, 0, len(countries[c]))
		for name := range countries[c] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			tt := countries[c][name]
			mins := tt.seconds / 60
			if mins < minMinutes {
				continue
			}
			t.Cities = append(t.Cities, City{City: name, Points: tt.points, Timestamp: tt.timestamp, StayedFor: mins})
		}
		out = append(out, t)
	}
	return out
}

// CalculateMonth recomputes the stat row for one user/month.
func CalculateMonth(ctx context.Context, s *store.Store, u *store.User, year, month int) error {
	tz := u.Timezone()
	if _, err := time.LoadLocation(tz); err != nil {
		tz = "UTC"
	}
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -2).Unix()
	end := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 2).Unix()
	gap := int64(u.Setting("minutes_between_routes")) * 60

	var count int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM points WHERE user_id=$1 AND anomaly IS NOT TRUE
		AND timestamp BETWEEN $2 AND $3`, u.ID, start, end).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		_, err := s.Pool.Exec(ctx, `UPDATE stats SET daily_distance='{}', distance=0, toponyms='[]', h3_hex_ids='{}',
			calculation_version=$4, updated_at=now() WHERE user_id=$1 AND year=$2 AND month=$3`, u.ID, year, month, CalculationVersion)
		return err
	}

	rows, err := s.Pool.Query(ctx, `
WITH pts AS (
  SELECT timestamp, lonlat, lag(lonlat) OVER w AS prev, lag(timestamp) OVER w AS prev_ts
  FROM points WHERE user_id=$1 AND anomaly IS NOT TRUE AND timestamp BETWEEN $2 AND $3
  WINDOW w AS (ORDER BY timestamp))
SELECT extract(day FROM to_timestamp(timestamp) AT TIME ZONE $4)::int, round(sum(ST_Distance(lonlat, prev)))::bigint
FROM pts
WHERE prev IS NOT NULL AND timestamp - prev_ts <= $5
  AND extract(year FROM to_timestamp(timestamp) AT TIME ZONE $4) = $6
  AND extract(month FROM to_timestamp(timestamp) AT TIME ZONE $4) = $7
GROUP BY 1 ORDER BY 1`, u.ID, start, end, tz, gap, year, month)
	if err != nil {
		return err
	}
	var daily [][2]int64
	var total int64
	for rows.Next() {
		var d, m int64
		if err := rows.Scan(&d, &m); err != nil {
			rows.Close()
			return err
		}
		daily = append(daily, [2]int64{d, m})
		total += m
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	prows, err := s.Pool.Query(ctx, `SELECT id, timestamp, city, coalesce(country_name, country), coalesce(velocity::float8, 0)
		FROM points WHERE user_id=$1 AND anomaly IS NOT TRUE AND timestamp BETWEEN $2 AND $3
		AND extract(year FROM to_timestamp(timestamp) AT TIME ZONE $4) = $5
		AND extract(month FROM to_timestamp(timestamp) AT TIME ZONE $4) = $6
		AND city IS NOT NULL`, u.ID, start, end, tz, year, month)
	if err != nil {
		return err
	}
	var geo []GeoPoint
	for prows.Next() {
		var p GeoPoint
		var ts int32
		if err := prows.Scan(&p.ID, &ts, &p.City, &p.Country, &p.Velocity); err != nil {
			prows.Close()
			return err
		}
		p.Timestamp = int64(ts)
		geo = append(geo, p)
	}
	prows.Close()
	if err := prows.Err(); err != nil {
		return err
	}
	tops := Toponyms(geo, int64(u.Setting("min_minutes_spent_in_city")))

	dj, _ := json.Marshal(daily)
	tj, _ := json.Marshal(tops)
	_, err = s.Pool.Exec(ctx, `INSERT INTO stats (user_id, year, month, distance, daily_distance, toponyms, h3_hex_ids,
		calculation_version, flight_distance, sharing_uuid, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb,'{}'::jsonb,$7,0,gen_random_uuid(),now(),now())
		ON CONFLICT (user_id, year, month) DO UPDATE SET distance=excluded.distance,
		daily_distance=excluded.daily_distance, toponyms=excluded.toponyms,
		calculation_version=excluded.calculation_version, updated_at=now()`,
		u.ID, year, month, total, string(dj), string(tj), CalculationVersion)
	return err
}

// MonthsInRange lists distinct (year, month) pairs covering [minTS, maxTS] in the user's timezone.
func MonthsInRange(minTS, maxTS int64, tz string) [][2]int {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	a, b := time.Unix(minTS, 0).In(loc), time.Unix(maxTS, 0).In(loc)
	cur := time.Date(a.Year(), a.Month(), 1, 0, 0, 0, 0, loc)
	var out [][2]int
	for !cur.After(b) {
		out = append(out, [2]int{cur.Year(), int(cur.Month())})
		cur = cur.AddDate(0, 1, 0)
	}
	return out
}

// Summary is the payload of GET /api/v1/stats (StatsSerializer).
type YearStat struct {
	Year                  int              `json:"year"`
	TotalDistanceKm       int64            `json:"totalDistanceKm"`
	TotalCountriesVisited int              `json:"totalCountriesVisited"`
	TotalCitiesVisited    int              `json:"totalCitiesVisited"`
	MonthlyDistanceKm     map[string]int64 `json:"monthlyDistanceKm"`
}

type Summary struct {
	TotalDistanceKm            int64      `json:"totalDistanceKm"`
	TotalPointsTracked         int        `json:"totalPointsTracked"`
	TotalReverseGeocodedPoints int64      `json:"totalReverseGeocodedPoints"`
	TotalCountriesVisited      int        `json:"totalCountriesVisited"`
	TotalCitiesVisited         int        `json:"totalCitiesVisited"`
	YearlyStats                []YearStat `json:"yearlyStats"`
}

var monthNames = []string{"january", "february", "march", "april", "may", "june", "july", "august",
	"september", "october", "november", "december"}

func BuildSummary(ctx context.Context, s *store.Store, u *store.User) (*Summary, error) {
	rows, err := s.Pool.Query(ctx, `SELECT year, month, distance, coalesce(toponyms,'[]'::jsonb) FROM stats WHERE user_id=$1 ORDER BY year DESC, month`, u.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sum := &Summary{TotalPointsTracked: u.PointsCount, YearlyStats: []YearStat{}}
	allCountries, allCities := map[string]struct{}{}, map[string]struct{}{}
	type ys struct {
		stat      *YearStat
		countries map[string]struct{}
		cities    map[string]struct{}
	}
	years := map[int]*ys{}
	var order []int
	for rows.Next() {
		var y, m int
		var dist int64
		var raw []byte
		if err := rows.Scan(&y, &m, &dist, &raw); err != nil {
			return nil, err
		}
		e, ok := years[y]
		if !ok {
			e = &ys{stat: &YearStat{Year: y, MonthlyDistanceKm: map[string]int64{}}, countries: map[string]struct{}{}, cities: map[string]struct{}{}}
			for _, n := range monthNames {
				e.stat.MonthlyDistanceKm[n] = 0
			}
			years[y] = e
			order = append(order, y)
		}
		e.stat.MonthlyDistanceKm[monthNames[m-1]] = dist / 1000
		e.stat.TotalDistanceKm += dist / 1000
		sum.TotalDistanceKm += dist
		var tops []Toponym
		_ = json.Unmarshal(raw, &tops)
		for _, t := range tops {
			if t.Country == "" || len(t.Cities) == 0 {
				continue
			}
			e.countries[t.Country] = struct{}{}
			allCountries[t.Country] = struct{}{}
			for _, c := range t.Cities {
				if c.City != "" {
					e.cities[c.City] = struct{}{}
					allCities[c.City] = struct{}{}
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sum.TotalDistanceKm /= 1000
	sum.TotalCountriesVisited, sum.TotalCitiesVisited = len(allCountries), len(allCities)
	for _, y := range order {
		e := years[y]
		e.stat.TotalCountriesVisited, e.stat.TotalCitiesVisited = len(e.countries), len(e.cities)
		sum.YearlyStats = append(sum.YearlyStats, *e.stat)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM points WHERE user_id=$1 AND reverse_geocoded_at IS NOT NULL`, u.ID).
		Scan(&sum.TotalReverseGeocodedPoints); err != nil {
		return nil, err
	}
	return sum, nil
}
