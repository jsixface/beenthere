package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sixface/beenthere/internal/geo"
	"github.com/sixface/beenthere/internal/httpx"
	"github.com/sixface/beenthere/internal/store"
)

func nested(body map[string]any, key string) map[string]any {
	if m, ok := body[key].(map[string]any); ok {
		return m
	}
	return body
}

func optString(m map[string]any, k string) *string {
	if v, ok := m[k].(string); ok {
		return &v
	}
	return nil
}

func optFloat(m map[string]any, k string) *float64 {
	if v, ok := httpx.Float(m[k]); ok {
		return &v
	}
	return nil
}

func optTime(m map[string]any, k string) *time.Time {
	if v, ok := m[k].(string); ok {
		if t, ok := parseTime(v); ok {
			return &t
		}
	}
	return nil
}

func optInt64(m map[string]any, k string) *int64 {
	if f, ok := httpx.Float(m[k]); ok {
		n := int64(f)
		return &n
	}
	return nil
}

func int64s(v any) []int64 {
	var out []int64
	arr, _ := v.([]any)
	for _, e := range arr {
		switch t := e.(type) {
		case float64:
			out = append(out, int64(t))
		case string:
			if n, err := strconv.ParseInt(t, 10, 64); err == nil {
				out = append(out, n)
			}
		}
	}
	return out
}

func (s *Server) visitRoutes(mux *http.ServeMux) {
	p := "/api/v1/visits"
	mux.HandleFunc("GET "+p, s.auth(false, s.visitsIndex))
	mux.HandleFunc("GET "+p+"/{id}", s.auth(false, s.visitsShow))
	mux.HandleFunc("POST "+p, s.auth(false, s.visitsCreate))
	mux.HandleFunc("PATCH "+p+"/{id}", s.auth(false, s.visitsUpdate))
	mux.HandleFunc("PUT "+p+"/{id}", s.auth(false, s.visitsUpdate))
	mux.HandleFunc("DELETE "+p+"/{id}", s.auth(false, s.visitsDestroy))
	mux.HandleFunc("POST "+p+"/merge", s.auth(false, s.visitsMerge))
	mux.HandleFunc("POST "+p+"/bulk_update", s.auth(false, s.visitsBulkUpdate))
	mux.HandleFunc("POST "+p+"/batch", s.auth(false, s.visitsBatch))
}

func (s *Server) visitsIndex(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	f := store.VisitFilter{UserID: u.ID, Status: q.Get("status")}
	if q.Get("selection") == "true" && q.Get("sw_lat") != "" && q.Get("sw_lng") != "" && q.Get("ne_lat") != "" && q.Get("ne_lng") != "" {
		var b [4]float64
		for i, k := range []string{"sw_lng", "sw_lat", "ne_lng", "ne_lat"} {
			v, err := strconv.ParseFloat(q.Get(k), 64)
			if err != nil {
				httpx.Error(w, http.StatusBadRequest, "Invalid date format")
				return
			}
			b[i] = v
		}
		f.BBox = &b
	} else {
		from, ok1 := parseTime(q.Get("start_at"))
		to, ok2 := parseTime(q.Get("end_at"))
		if !ok1 || !ok2 {
			httpx.Error(w, http.StatusBadRequest, "Invalid date format")
			return
		}
		f.From, f.To = from, to
	}
	paged := q.Get("page") != ""
	if paged {
		f.Page = max(httpx.QueryInt(r, "page", 1), 1)
		f.PerPage = min(max(httpx.QueryInt(r, "per_page", 100), 1), 500)
	}
	list, total, err := s.S.ListVisits(r.Context(), f)
	if err != nil {
		s.fail(w, err)
		return
	}
	if paged {
		w.Header().Set("X-Current-Page", strconv.Itoa(f.Page))
		w.Header().Set("X-Total-Pages", strconv.Itoa((total+f.PerPage-1)/f.PerPage))
		w.Header().Set("X-Total-Count", strconv.Itoa(total))
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (s *Server) visitsShow(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	v, err := s.S.GetVisit(r.Context(), u.ID, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

func (s *Server) createVisit(r *http.Request, u *store.User, m map[string]any) (*store.VisitOut, bool, string) {
	lat, ok1 := httpx.Float(m["latitude"])
	lon, ok2 := httpx.Float(m["longitude"])
	if !ok1 || !ok2 {
		return nil, false, "Failed to create visit: invalid coordinates"
	}
	if !geo.ValidLonLat(lon, lat) {
		return nil, false, "Failed to create visit: coordinates out of range"
	}
	from, to := optTime(m, "started_at"), optTime(m, "ended_at")
	if from == nil || to == nil {
		return nil, false, "Failed to create visit: invalid timestamps"
	}
	if !to.After(*from) {
		return nil, false, "Failed to create visit: ended_at must be after started_at"
	}
	name, _ := m["name"].(string)
	status, _ := m["status"].(string)
	v, dup, err := s.S.CreateVisit(r.Context(), u.ID, name, lat, lon, *from, *to, status)
	if err != nil {
		return nil, false, "Failed to create visit: " + err.Error()
	}
	return v, dup, ""
}

func (s *Server) visitsCreate(w http.ResponseWriter, r *http.Request, u *store.User) {
	body, err := httpx.DecodeBody(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	v, _, msg := s.createVisit(r, u, nested(body, "visit"))
	if msg != "" {
		httpx.Error(w, http.StatusUnprocessableEntity, msg)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

func (s *Server) visitsBatch(w http.ResponseWriter, r *http.Request, u *store.User) {
	body, err := httpx.DecodeBody(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	items, _ := body["visits"].([]any)
	if len(items) == 0 {
		httpx.Error(w, http.StatusUnprocessableEntity, "No visits provided")
		return
	}
	if len(items) > 100 {
		httpx.JSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": "Too many visits, maximum is 100 per batch", "limit": 100, "requested": len(items)})
		return
	}
	results := []map[string]any{}
	created, dups, failed := 0, 0, 0
	for i, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			results = append(results, map[string]any{"index": i, "status": "failed", "error": "Invalid visit payload"})
			failed++
			continue
		}
		v, dup, msg := s.createVisit(r, u, m)
		switch {
		case msg != "":
			results = append(results, map[string]any{"index": i, "status": "failed", "error": msg})
			failed++
		case dup:
			results = append(results, map[string]any{"index": i, "status": "duplicate", "visit": v})
			dups++
		default:
			results = append(results, map[string]any{"index": i, "status": "created", "visit": v})
			created++
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"results": results, "created_count": created, "duplicate_count": dups, "failed_count": failed})
}

func (s *Server) visitsUpdate(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	body, err := httpx.DecodeBody(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	m := nested(body, "visit")
	cur, err := s.S.GetVisit(r.Context(), u.ID, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	patch := store.VisitPatch{Name: optString(m, "name"), Status: optString(m, "status"),
		StartedAt: optTime(m, "started_at"), EndedAt: optTime(m, "ended_at")}
	if pid := optInt64(m, "place_id"); pid != nil {
		pl, err := s.S.GetPlace(r.Context(), u.ID, *pid)
		if err != nil {
			httpx.Error(w, http.StatusUnprocessableEntity, "Invalid place")
			return
		}
		patch.PlaceID = pid
		if patch.Name == nil || *patch.Name == "" {
			patch.Name = &pl.Name
		}
	}
	if aid := optInt64(m, "area_id"); aid != nil {
		a, err := s.S.GetArea(r.Context(), u.ID, *aid)
		if err != nil {
			httpx.Error(w, http.StatusUnprocessableEntity, "Invalid area")
			return
		}
		patch.AreaID = aid
		if (patch.Name == nil || *patch.Name == "") && a.Name != "" {
			patch.Name = &a.Name
		}
	}
	if patch.Status == nil && cur.Status == "suggested" {
		c := "confirmed"
		patch.Status = &c
	}
	v, err := s.S.UpdateVisit(r.Context(), u.ID, id, patch)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

func (s *Server) visitsDestroy(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	if err := s.S.SoftDeleteVisit(r.Context(), u.ID, id); err != nil {
		httpx.Error(w, http.StatusNotFound, "Visit not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) visitsMerge(w http.ResponseWriter, r *http.Request, u *store.User) {
	body, _ := httpx.DecodeBody(r)
	ids := int64s(body["visit_ids"])
	if len(ids) < 2 {
		httpx.Error(w, http.StatusUnprocessableEntity, "At least 2 visits must be selected for merging")
		return
	}
	v, err := s.S.MergeVisits(r.Context(), u.ID, ids)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "One or more visits not found")
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

func (s *Server) visitsBulkUpdate(w http.ResponseWriter, r *http.Request, u *store.User) {
	body, _ := httpx.DecodeBody(r)
	status, _ := body["status"].(string)
	ids := int64s(body["visit_ids"])
	if len(ids) == 0 {
		httpx.Error(w, http.StatusUnprocessableEntity, "No visits selected")
		return
	}
	n, err := s.S.BulkUpdateVisitStatus(r.Context(), u.ID, ids, status)
	if err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, "Invalid status")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"message": strconv.FormatInt(n, 10) + " visits updated successfully", "updated_count": n})
}

// ---- places ----

func (s *Server) placeRoutes(mux *http.ServeMux) {
	p := "/api/v1/places"
	mux.HandleFunc("GET "+p, s.auth(false, s.placesIndex))
	mux.HandleFunc("GET "+p+"/nearby", s.auth(false, s.placesNearby))
	mux.HandleFunc("GET "+p+"/search", s.auth(false, s.placesSearch))
	mux.HandleFunc("GET "+p+"/{id}", s.auth(false, s.placesShow))
	mux.HandleFunc("POST "+p, s.auth(false, s.placesCreate))
	mux.HandleFunc("PATCH "+p+"/{id}", s.auth(false, s.placesUpdate))
	mux.HandleFunc("PUT "+p+"/{id}", s.auth(false, s.placesUpdate))
	mux.HandleFunc("DELETE "+p+"/{id}", s.auth(false, s.placesDestroy))
}

func (s *Server) placesIndex(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	f := store.PlaceFilter{UserID: u.ID, Filter: q.Get("filter")}
	for _, k := range []string{"tag_ids", "tag_ids[]"} {
		for _, v := range q[k] {
			for _, part := range strings.Split(v, ",") {
				if part == "untagged" {
					f.Untagged = true
				} else if n, err := strconv.ParseInt(part, 10, 64); err == nil {
					f.TagIDs = append(f.TagIDs, n)
				}
			}
		}
	}
	paged := q.Get("page") != ""
	if paged {
		f.Page = max(httpx.QueryInt(r, "page", 1), 1)
		f.PerPage = min(max(httpx.QueryInt(r, "per_page", 100), 1), 500)
	}
	list, total, err := s.S.ListPlaces(r.Context(), f)
	if err != nil {
		s.fail(w, err)
		return
	}
	pages := 1
	if paged {
		pages = (total + f.PerPage - 1) / f.PerPage
		w.Header().Set("X-Current-Page", strconv.Itoa(f.Page))
	} else {
		w.Header().Set("X-Current-Page", "1")
	}
	w.Header().Set("X-Total-Pages", strconv.Itoa(pages))
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	httpx.JSON(w, http.StatusOK, list)
}

func (s *Server) placesShow(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	p, err := s.S.GetPlace(r.Context(), u.ID, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, p)
}

func (s *Server) placesCreate(w http.ResponseWriter, r *http.Request, u *store.User) {
	body, err := httpx.DecodeBody(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	m := nested(body, "place")
	name, _ := m["name"].(string)
	lat, ok1 := httpx.Float(m["latitude"])
	lon, ok2 := httpx.Float(m["longitude"])
	if name == "" || !ok1 || !ok2 || !geo.ValidLonLat(lon, lat) {
		httpx.Errors(w, http.StatusUnprocessableEntity, "Name can't be blank", "Lonlat can't be blank")
		return
	}
	note, _ := m["note"].(string)
	p, err := s.S.CreatePlace(r.Context(), u.ID, name, lat, lon, 0, note, int64s(m["tag_ids"]))
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, p)
}

func (s *Server) placesUpdate(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	body, err := httpx.DecodeBody(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	m := nested(body, "place")
	patch := store.PlacePatch{Name: optString(m, "name"), Lat: optFloat(m, "latitude"), Lon: optFloat(m, "longitude"), Note: optString(m, "note")}
	if _, ok := m["tag_ids"]; ok {
		ids := int64s(m["tag_ids"])
		patch.TagIDs = &ids
	}
	p, err := s.S.UpdatePlace(r.Context(), u.ID, id, patch)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, p)
}

func (s *Server) placesDestroy(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	if err := s.S.DeletePlace(r.Context(), u.ID, id); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) placesNearby(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	lat, e1 := strconv.ParseFloat(q.Get("latitude"), 64)
	lon, e2 := strconv.ParseFloat(q.Get("longitude"), 64)
	if e1 != nil || e2 != nil {
		httpx.Error(w, http.StatusBadRequest, "Latitude and longitude are required")
		return
	}
	radius := 0.5
	if v, err := strconv.ParseFloat(q.Get("radius"), 64); err == nil {
		radius = v
	}
	res, err := s.S.NearbyPlaces(r.Context(), u.ID, lat, lon, radius, httpx.QueryInt(r, "limit", 10), "")
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"places": res})
}

// placesSearch searches the user's own places (external geocoder search is not part of this build).
func (s *Server) placesSearch(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	lat, e1 := strconv.ParseFloat(q.Get("lat"), 64)
	lon, e2 := strconv.ParseFloat(q.Get("lon"), 64)
	if e1 != nil || e2 != nil {
		httpx.Error(w, http.StatusBadRequest, "Lat and lon are required")
		return
	}
	if !geo.ValidLonLat(lon, lat) {
		httpx.Error(w, http.StatusBadRequest, "Invalid coordinates")
		return
	}
	radius := 1.0
	if v, err := strconv.ParseFloat(q.Get("radius"), 64); err == nil {
		radius = min(max(v, 0.01), 5)
	}
	limit := min(max(httpx.QueryInt(r, "limit", 10), 1), 50)
	res, err := s.S.NearbyPlaces(r.Context(), u.ID, lat, lon, radius, limit, strings.TrimSpace(q.Get("q")))
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"places": res, "areas": []any{}})
}

// ---- notes and tags ----

func (s *Server) noteTagRoutes(mux *http.ServeMux) {
	n := "/api/v1/notes"
	mux.HandleFunc("GET "+n, s.auth(false, s.notesIndex))
	mux.HandleFunc("GET "+n+"/{id}", s.auth(false, s.notesShow))
	mux.HandleFunc("POST "+n, s.auth(false, s.notesCreate))
	mux.HandleFunc("PATCH "+n+"/{id}", s.auth(false, s.notesUpdate))
	mux.HandleFunc("PUT "+n+"/{id}", s.auth(false, s.notesUpdate))
	mux.HandleFunc("DELETE "+n+"/{id}", s.auth(false, s.notesDestroy))

	mux.HandleFunc("GET /api/v1/tags/privacy_zones", s.auth(false, func(w http.ResponseWriter, r *http.Request, u *store.User) {
		z, err := s.S.PrivacyZones(r.Context(), u.ID)
		if err != nil {
			s.fail(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, z)
	}))
	// Tag management (the Rails app does this through HTML forms only).
	mux.HandleFunc("GET /api/v1/tags", s.auth(false, func(w http.ResponseWriter, r *http.Request, u *store.User) {
		t, err := s.S.ListTags(r.Context(), u.ID)
		if err != nil {
			s.fail(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, t)
	}))
	mux.HandleFunc("POST /api/v1/tags", s.auth(false, s.tagSave(false)))
	mux.HandleFunc("PATCH /api/v1/tags/{id}", s.auth(false, s.tagSave(true)))
	mux.HandleFunc("DELETE /api/v1/tags/{id}", s.auth(false, func(w http.ResponseWriter, r *http.Request, u *store.User) {
		id, _ := httpx.PathID(r, "id")
		if err := s.S.DeleteTag(r.Context(), u.ID, id); err != nil {
			s.fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

func (s *Server) tagSave(update bool) userHandler {
	return func(w http.ResponseWriter, r *http.Request, u *store.User) {
		body, err := httpx.DecodeBody(r)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		m := nested(body, "tag")
		var id int64
		if update {
			id, _ = httpx.PathID(r, "id")
		}
		name, _ := m["name"].(string)
		if name == "" {
			httpx.Errors(w, http.StatusUnprocessableEntity, "Name can't be blank")
			return
		}
		var radius *int
		if f, ok := httpx.Float(m["privacy_radius_meters"]); ok {
			if f <= 0 || f > 5000 {
				httpx.Errors(w, http.StatusUnprocessableEntity, "Privacy radius meters must be between 1 and 5000")
				return
			}
			n := int(f)
			radius = &n
		}
		if c := optString(m, "color"); c != nil && *c != "" && !validColor(*c) {
			httpx.Errors(w, http.StatusUnprocessableEntity, "Color is invalid")
			return
		}
		t, err := s.S.UpsertTag(r.Context(), u.ID, id, name, optString(m, "icon"), optString(m, "color"), radius)
		if err != nil {
			httpx.Errors(w, http.StatusUnprocessableEntity, "Name has already been taken")
			return
		}
		status := http.StatusCreated
		if update {
			status = http.StatusOK
		}
		httpx.JSON(w, status, t)
	}
}

func validColor(c string) bool {
	if len(c) != 4 && len(c) != 7 || c[0] != '#' {
		return false
	}
	for _, ch := range c[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", ch) {
			return false
		}
	}
	return true
}

func noteInput(m map[string]any) store.NoteIn {
	n := store.NoteIn{Title: optString(m, "title"), Body: optString(m, "body"), Lat: optFloat(m, "latitude"),
		Lon: optFloat(m, "longitude"), AttachableType: optString(m, "attachable_type"), AttachableID: optInt64(m, "attachable_id"),
		NotedAt: optTime(m, "noted_at")}
	if n.NotedAt == nil {
		if d := optString(m, "date"); d != nil {
			if t, ok := parseTime(*d); ok {
				t = time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, time.UTC)
				n.NotedAt = &t
			}
		}
	}
	return n
}

func (s *Server) notesIndex(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	var aid *int64
	if v, err := strconv.ParseInt(q.Get("attachable_id"), 10, 64); err == nil {
		aid = &v
	}
	list, err := s.S.ListNotes(r.Context(), u.ID, q.Get("attachable_type"), aid, q.Get("standalone") == "true")
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (s *Server) notesShow(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	n, err := s.S.GetNote(r.Context(), u.ID, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, n)
}

func (s *Server) notesCreate(w http.ResponseWriter, r *http.Request, u *store.User) {
	body, err := httpx.DecodeBody(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	in := noteInput(nested(body, "note"))
	if in.Body == nil || strings.TrimSpace(*in.Body) == "" || len(*in.Body) > 10000 {
		httpx.Errors(w, http.StatusUnprocessableEntity, "Body can't be blank")
		return
	}
	if in.AttachableType != nil && *in.AttachableType != "" {
		switch *in.AttachableType {
		case "Trip", "Area", "Visit", "Place":
		default:
			httpx.Errors(w, http.StatusUnprocessableEntity, "Attachable type is not included in the list")
			return
		}
	}
	n, err := s.S.CreateNote(r.Context(), u.ID, in)
	if err != nil {
		httpx.Errors(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, n)
}

func (s *Server) notesUpdate(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	body, err := httpx.DecodeBody(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	n, err := s.S.UpdateNote(r.Context(), u.ID, id, noteInput(nested(body, "note")))
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, n)
}

func (s *Server) notesDestroy(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	if err := s.S.DeleteNote(r.Context(), u.ID, id); err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"message": "Note was successfully deleted"})
}

// ---- trips (JSON API; the Rails app exposes trips only through HTML pages) ----

func (s *Server) tripRoutes(mux *http.ServeMux) {
	p := "/api/v1/trips"
	mux.HandleFunc("GET "+p, s.auth(false, func(w http.ResponseWriter, r *http.Request, u *store.User) {
		t, err := s.S.ListTrips(r.Context(), u.ID)
		if err != nil {
			s.fail(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, t)
	}))
	mux.HandleFunc("GET "+p+"/{id}", s.auth(false, func(w http.ResponseWriter, r *http.Request, u *store.User) {
		id, _ := httpx.PathID(r, "id")
		t, err := s.S.GetTrip(r.Context(), u.ID, id)
		if err != nil {
			s.fail(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, t)
	}))
	mux.HandleFunc("POST "+p, s.auth(false, s.tripsCreate))
	mux.HandleFunc("PATCH "+p+"/{id}", s.auth(false, s.tripsUpdate))
	mux.HandleFunc("PUT "+p+"/{id}", s.auth(false, s.tripsUpdate))
	mux.HandleFunc("DELETE "+p+"/{id}", s.auth(false, func(w http.ResponseWriter, r *http.Request, u *store.User) {
		id, _ := httpx.PathID(r, "id")
		if err := s.S.DeleteTrip(r.Context(), u.ID, id); err != nil {
			s.fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("POST "+p+"/{id}/recalculate", s.auth(false, func(w http.ResponseWriter, r *http.Request, u *store.User) {
		id, _ := httpx.PathID(r, "id")
		if err := s.S.RecalculateTrip(r.Context(), u.ID, id); err != nil {
			s.fail(w, err)
			return
		}
		t, err := s.S.GetTrip(r.Context(), u.ID, id)
		if err != nil {
			s.fail(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, t)
	}))
}

func (s *Server) tripsCreate(w http.ResponseWriter, r *http.Request, u *store.User) {
	body, err := httpx.DecodeBody(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	m := nested(body, "trip")
	name, _ := m["name"].(string)
	from, to := optTime(m, "started_at"), optTime(m, "ended_at")
	if name == "" || from == nil || to == nil {
		httpx.Errors(w, http.StatusUnprocessableEntity, "Name, started at and ended at are required")
		return
	}
	if !from.Before(*to) {
		httpx.Errors(w, http.StatusUnprocessableEntity, "Started at must be before ended at")
		return
	}
	desc, _ := m["description"].(string)
	t, err := s.S.CreateTrip(r.Context(), u.ID, name, *from, *to, desc)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, t)
}

func (s *Server) tripsUpdate(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	body, err := httpx.DecodeBody(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	m := nested(body, "trip")
	from, to := optTime(m, "started_at"), optTime(m, "ended_at")
	if from != nil && to != nil && !from.Before(*to) {
		httpx.Errors(w, http.StatusUnprocessableEntity, "Started at must be before ended at")
		return
	}
	t, err := s.S.UpdateTrip(r.Context(), u.ID, id, optString(m, "name"), from, to, optString(m, "description"))
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, t)
}
