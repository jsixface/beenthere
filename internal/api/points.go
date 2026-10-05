package api

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sixface/beenthere/internal/geo"
	"github.com/sixface/beenthere/internal/httpx"
	"github.com/sixface/beenthere/internal/ingest"
	"github.com/sixface/beenthere/internal/store"
)

const (
	bulkDestroyMax = 5000
	maxPerPage     = 10000
	maxBody        = 64 << 20
)

// safeTimestamp mirrors SafeTimestampParser: numeric strings are unix seconds,
// anything else is parsed as a date; unparseable input means "now".
func safeTimestamp(v string) int64 {
	if v == "" {
		return time.Now().Unix()
	}
	lo, hi := int64(0), int64(4102444800)
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return min(max(n, lo), hi)
	}
	for _, l := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(l, v); err == nil {
			return min(max(t.Unix(), lo), hi)
		}
	}
	return time.Now().Unix()
}

func parseBBox(r *http.Request) (*[4]float64, bool, bool) {
	q := r.URL.Query()
	if q.Get("min_longitude") == "" || q.Get("max_longitude") == "" || q.Get("min_latitude") == "" || q.Get("max_latitude") == "" {
		return nil, false, true
	}
	var v [4]float64
	for i, k := range []string{"min_longitude", "min_latitude", "max_longitude", "max_latitude"} {
		f, err := strconv.ParseFloat(q.Get(k), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, true, false
		}
		v[i] = f
	}
	if v[0] > v[2] || v[1] > v[3] || v[0] < -180 || v[2] > 180 || v[1] < -90 || v[3] > 90 {
		return nil, true, false
	}
	return &v, true, true
}

func (s *Server) pointsIndex(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	f := store.PointFilter{UserID: u.ID, StartAt: math.MinInt32, EndAt: time.Now().Unix(), Desc: q.Get("order") != "asc"}
	if v := q.Get("start_at"); v != "" {
		f.StartAt = safeTimestamp(v)
	}
	if v := q.Get("end_at"); v != "" {
		f.EndAt = safeTimestamp(v)
	}
	f.AnomaliesOnly = httpx.QueryBool(r, "anomalies_only")
	f.IncludeAnomalies = httpx.QueryBool(r, "include_anomalies")
	if v := q.Get("import_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.ImportID = &n
		}
	}
	bbox, present, ok := parseBBox(r)
	if present && !ok {
		httpx.Error(w, http.StatusBadRequest, "Invalid bounding box")
		return
	}
	f.BBox = bbox
	f.Page = max(httpx.QueryInt(r, "page", 1), 1)
	f.PerPage = httpx.QueryInt(r, "per_page", 100)
	if f.PerPage <= 0 {
		f.PerPage = 100
	}
	f.PerPage = min(f.PerPage, maxPerPage)
	slim := q.Get("slim") == "true"

	count, maxTS, maxUpd, err := s.S.CountPoints(r.Context(), f)
	if err != nil {
		s.fail(w, err)
		return
	}
	var ts int64
	if maxTS != nil {
		ts = *maxTS
	}
	var upd int64
	if maxUpd != nil {
		upd = maxUpd.UnixNano()
	}
	var imp, trk int64
	if f.ImportID != nil {
		imp = *f.ImportID
	}
	if f.TrackID != nil {
		trk = *f.TrackID
	}
	h := sha1.New()
	fmt.Fprint(h, "points/index|", u.ID, f.StartAt, f.EndAt, f.Desc, f.AnomaliesOnly, f.IncludeAnomalies, f.Page, f.PerPage, imp, trk)
	if f.BBox != nil {
		fmt.Fprint(h, *f.BBox)
	}
	fmt.Fprint(h, slim, count, ts, upd)
	etag := `W/"` + hex.EncodeToString(h.Sum(nil)) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private")
	if maxTS != nil {
		w.Header().Set("Last-Modified", time.Unix(*maxTS, 0).UTC().Format(http.TimeFormat))
	}
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	totalPages := int(math.Ceil(float64(count) / float64(f.PerPage)))
	w.Header().Set("X-Current-Page", strconv.Itoa(f.Page))
	w.Header().Set("X-Total-Pages", strconv.Itoa(totalPages))
	if slim {
		pts, err := s.S.ListSlimPoints(r.Context(), f)
		if err != nil {
			s.fail(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, pts)
		return
	}
	pts, err := s.S.ListPoints(r.Context(), f)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pts)
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "request body too large")
		return nil, false
	}
	return b, true
}

// ingestPoints writes points and schedules post-processing.
func (s *Server) ingestPoints(w http.ResponseWriter, r *http.Request, u *store.User, pts []store.PointIn) (*store.UpsertResult, bool) {
	res, err := s.S.UpsertPoints(r.Context(), u.ID, pts)
	if err != nil {
		s.Log.Error("point creation failed", "user", u.ID, "err", err)
		httpx.Error(w, http.StatusInternalServerError, "Point creation failed")
		return nil, false
	}
	if len(res.IDs) > 0 {
		lo, hi := res.Timestamps[0], res.Timestamps[0]
		for _, t := range res.Timestamps {
			lo, hi = min(lo, t), max(hi, t)
		}
		s.Jobs.PointsArrived(u.ID, res.IDs, lo, hi)
	}
	return res, true
}

func (s *Server) pointsCreate(w http.ResponseWriter, r *http.Request, u *store.User) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	pts, err := ingest.ParseGeoJSONLocations(body)
	if err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	res, ok := s.ingestPoints(w, r, u, pts)
	if !ok {
		return
	}
	data := make([]map[string]any, len(res.IDs))
	for i := range res.IDs {
		data[i] = map[string]any{"id": res.IDs[i], "timestamp": res.Timestamps[i],
			"longitude": res.Lons[i], "latitude": res.Lats[i]}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": data})
}

func (s *Server) overland(w http.ResponseWriter, r *http.Request, u *store.User) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	pts, err := ingest.ParseGeoJSONLocations(body)
	if err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if len(pts) > 0 {
		if _, ok := s.ingestPoints(w, r, u, pts); !ok {
			return
		}
	}
	httpx.JSON(w, http.StatusCreated, map[string]string{"result": "ok"})
}

func (s *Server) owntracks(w http.ResponseWriter, r *http.Request, u *store.User) {
	m, err := httpx.DecodeBody(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if p, ok := ingest.OwnTracksPoint(m); ok {
		if _, ok := s.ingestPoints(w, r, u, []store.PointIn{p}); !ok {
			return
		}
	}
	// OwnTracks expects a list of friend locations (none supported yet).
	httpx.JSON(w, http.StatusOK, []any{})
}

func (s *Server) traccar(w http.ResponseWriter, r *http.Request, u *store.User) {
	m, err := httpx.DecodeBody(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid payload")
		return
	}
	// Traccar also posts form-encoded / query-string requests.
	for k, v := range r.URL.Query() {
		if _, ok := m[k]; !ok && len(v) > 0 {
			m[k] = v[0]
		}
	}
	p, ok := ingest.TraccarPoint(m)
	if !ok {
		httpx.Error(w, http.StatusUnprocessableEntity, "Point creation failed")
		return
	}
	if _, ok := s.ingestPoints(w, r, u, []store.PointIn{p}); !ok {
		return
	}
	httpx.JSON(w, http.StatusOK, []any{})
}

func (s *Server) pointsUpdate(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, ok := httpx.PathID(r, "id")
	if !ok {
		httpx.JSON(w, http.StatusNotFound, map[string]string{"error": "Record not found"})
		return
	}
	body, err := httpx.DecodeBody(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	pm, _ := body["point"].(map[string]any)
	lat, ok1 := httpx.Float(pm["latitude"])
	lon, ok2 := httpx.Float(pm["longitude"])
	if !ok1 || !ok2 || !geo.ValidLonLat(lon, lat) {
		httpx.Error(w, http.StatusUnprocessableEntity, "Lonlat is invalid")
		return
	}
	track, err := s.S.MovePoint(r.Context(), u.ID, id, lon, lat)
	if err != nil {
		s.fail(w, err)
		return
	}
	if track != nil {
		_ = s.S.Tx(r.Context(), func(tx pgxTx) error { return store.RefreshTrackFromPoints(r.Context(), tx, u.ID, *track) })
	}
	pts, err := s.S.ListPoints(r.Context(), store.PointFilter{UserID: u.ID, StartAt: math.MinInt32, EndAt: math.MaxInt32,
		IncludeAnomalies: true, Page: 1, PerPage: 1, PointID: &id})
	if err != nil || len(pts) == 0 {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pts[0])
}

func (s *Server) pointsDestroy(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, ok := httpx.PathID(r, "id")
	if !ok {
		httpx.JSON(w, http.StatusNotFound, map[string]string{"error": "Record not found"})
		return
	}
	n, err := s.S.DeletePoints(r.Context(), u.ID, []int64{id})
	if err != nil {
		s.fail(w, err)
		return
	}
	if n == 0 {
		httpx.JSON(w, http.StatusNotFound, map[string]string{"error": "Record not found"})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"message": "Point deleted successfully"})
}

func (s *Server) pointsBulkDestroy(w http.ResponseWriter, r *http.Request, u *store.User) {
	body, err := httpx.DecodeBody(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	var ids []int64
	if arr, ok := body["point_ids"].([]any); ok {
		for _, v := range arr {
			switch t := v.(type) {
			case float64:
				ids = append(ids, int64(t))
			case string:
				if n, err := strconv.ParseInt(t, 10, 64); err == nil {
					ids = append(ids, n)
				}
			}
		}
	}
	if len(ids) == 0 {
		httpx.Error(w, http.StatusUnprocessableEntity, "No points selected")
		return
	}
	if len(ids) > bulkDestroyMax {
		httpx.JSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": fmt.Sprintf("Too many points selected, maximum is %d per request", bulkDestroyMax),
			"limit": bulkDestroyMax, "requested": len(ids)})
		return
	}
	n, err := s.S.DeletePoints(r.Context(), u.ID, ids)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"message": "Points were successfully destroyed", "count": n})
}

var monthNames = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

func (s *Server) trackedMonths(w http.ResponseWriter, r *http.Request, u *store.User) {
	m, err := s.S.TrackedMonths(r.Context(), u.ID, u.Timezone())
	if err != nil {
		s.fail(w, err)
		return
	}
	out := map[string][]string{}
	for y, ms := range m {
		names := make([]string, len(ms))
		for i, mo := range ms {
			names[i] = monthNames[mo-1]
		}
		out[strconv.Itoa(y)] = names
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	if err == store.ErrNotFound {
		httpx.JSON(w, http.StatusNotFound, map[string]string{"error": "Record not found"})
		return
	}
	s.Log.Error("request failed", "err", err)
	httpx.Error(w, http.StatusInternalServerError, "Internal server error")
}

func csvInts(s string) []int64 {
	var out []int64
	for _, p := range strings.Split(s, ",") {
		if n, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil {
			out = append(out, n)
		}
	}
	return out
}
