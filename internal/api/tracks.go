package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sixface/beenthere/internal/httpx"
	"github.com/sixface/beenthere/internal/store"
)

var modeEmoji = []string{"❓", "🚶", "🏃", "🚴", "🚗", "🚌", "🚆", "✈️", "⛵", "🏍️", "📍"}

func trackFeature(t *store.Track) map[string]any {
	emoji := "❓"
	if t.DominantMode >= 0 && t.DominantMode < len(modeEmoji) {
		emoji = modeEmoji[t.DominantMode]
	}
	return map[string]any{
		"type": "Feature", "geometry": t.Geometry,
		"properties": map[string]any{
			"id": t.ID, "color": "#6366F1",
			"start_at": t.StartAt.UTC().Format(time.RFC3339), "end_at": t.EndAt.UTC().Format(time.RFC3339),
			"distance": t.Distance, "avg_speed": t.AvgSpeed, "duration": t.Duration, "revision": t.Revision,
			"dominant_mode": t.DominantMode, "dominant_mode_emoji": emoji,
			"mode_timeline": []any{}, "segments": []any{},
		},
	}
}

func parseTime(v string) (time.Time, bool) {
	if v == "" {
		return time.Time{}, false
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Unix(n, 0).UTC(), true
	}
	for _, l := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(l, v); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func (s *Server) tracksIndex(w http.ResponseWriter, r *http.Request, u *store.User) {
	page := max(httpx.QueryInt(r, "page", 1), 1)
	per := httpx.QueryInt(r, "per_page", 500)
	if per <= 0 {
		per = 500
	}
	var from, to *time.Time
	if a, ok := parseTime(r.URL.Query().Get("start_at")); ok {
		if b, ok := parseTime(r.URL.Query().Get("end_at")); ok {
			from, to = &a, &b
		}
	}
	list, total, err := s.S.ListTracks(r.Context(), u.ID, from, to, per, (page-1)*per)
	if err != nil {
		s.fail(w, err)
		return
	}
	feats := make([]map[string]any, len(list))
	for i := range list {
		feats[i] = trackFeature(&list[i])
	}
	w.Header().Set("X-Current-Page", strconv.Itoa(page))
	w.Header().Set("X-Total-Pages", strconv.Itoa((total+per-1)/per))
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	httpx.JSON(w, http.StatusOK, map[string]any{"type": "FeatureCollection", "features": feats})
}

func (s *Server) tracksShow(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	t, err := s.S.GetTrack(r.Context(), u.ID, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"type": "FeatureCollection", "features": []any{trackFeature(t)}})
}

func (s *Server) tracksPoints(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	if _, err := s.S.GetTrack(r.Context(), u.ID, id); err != nil {
		s.fail(w, err)
		return
	}
	f := store.PointFilter{UserID: u.ID, StartAt: -1 << 31, EndAt: 1<<31 - 1, TrackID: &id, Page: 1, PerPage: 1000000}
	if r.URL.Query().Get("page") != "" {
		f.Page = max(httpx.QueryInt(r, "page", 1), 1)
		f.PerPage = min(max(httpx.QueryInt(r, "per_page", 1000), 1), 1000)
		count, _, _, err := s.S.CountPoints(r.Context(), f)
		if err == nil {
			w.Header().Set("X-Current-Page", strconv.Itoa(f.Page))
			w.Header().Set("X-Total-Pages", strconv.Itoa(int((count+int64(f.PerPage)-1)/int64(f.PerPage))))
		}
	}
	if v := r.URL.Query().Get("import_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.ImportID = &n
		}
	}
	pts, err := s.S.ListPoints(r.Context(), f)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pts)
}

var _ = fmt.Sprint
var _ = strings.TrimSpace
var _ json.RawMessage
