package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sixface/beenthere/internal/httpx"
	"github.com/sixface/beenthere/internal/stats"
	"github.com/sixface/beenthere/internal/store"
)

type pgxTx = pgx.Tx

func (s *Server) usersMe(w http.ResponseWriter, r *http.Request, u *store.User) {
	st := u.EffectiveSettings()
	pick := func(keys ...string) map[string]any {
		out := map[string]any{}
		for _, k := range keys {
			out[k] = st[k]
		}
		return out
	}
	settings := pick("maps", "preferred_map_layer", "speed_colored_routes", "points_rendering_mode",
		"live_map_enabled", "immich_url", "photoprism_url", "speed_color_scale", "fog_of_war_threshold", "globe_projection")
	settings["timezone"] = u.Timezone()
	settings["fog_of_war_meters"] = u.Setting("fog_of_war_meters")
	settings["meters_between_routes"] = u.Setting("meters_between_routes")
	settings["minutes_between_routes"] = u.Setting("minutes_between_routes")
	settings["time_threshold_minutes"] = u.Setting("time_threshold_minutes")
	settings["merge_threshold_minutes"] = u.Setting("merge_threshold_minutes")
	settings["route_opacity"] = st["route_opacity"]
	settings["visits_suggestions_enabled"] = st["visits_suggestions_enabled"] != false && st["visits_suggestions_enabled"] != "false"
	httpx.JSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id": u.ID, "email": u.Email, "theme": u.Theme, "created_at": u.CreatedAt, "updated_at": u.CreatedAt,
			"settings": settings,
		},
		"features": map[string]any{},
	})
}

func (s *Server) settingsIndex(w http.ResponseWriter, r *http.Request, u *store.User) {
	httpx.JSON(w, http.StatusOK, map[string]any{"settings": u.EffectiveSettings(), "status": "success"})
}

var settingKeys = map[string]bool{
	"timezone": true, "meters_between_routes": true, "minutes_between_routes": true, "fog_of_war_meters": true,
	"time_threshold_minutes": true, "merge_threshold_minutes": true, "route_opacity": true, "route_color": true,
	"track_color": true, "preferred_map_layer": true, "points_rendering_mode": true, "live_map_enabled": true,
	"immich_url": true, "immich_api_key": true, "photoprism_url": true, "photoprism_api_key": true,
	"speed_colored_routes": true, "speed_color_scale": true, "fog_of_war_threshold": true, "fog_of_war_mode": true,
	"maps_v2_style": true, "maps_maplibre_style": true, "maps_maplibre_tiles_url": true,
	"maps_maplibre_tiles_fallback": true, "globe_projection": true, "min_minutes_spent_in_city": true,
	"gps_filtering_enabled": true, "point_dragging_enabled": true, "points_tiled_rendering": true,
	"enabled_map_layers": true, "places_tag_filters": true, "enabled_transportation_modes": true,
	"maps_maplibre_custom_theme": true, "maps": true, "visits_suggestions_enabled": true,
	"visit_radius_meters": true, "visit_min_points": true, "visit_min_duration_minutes": true,
}

func validTilesURL(v any) bool {
	if v == nil {
		return true
	}
	str, ok := v.(string)
	if !ok {
		return false
	}
	if strings.Contains(str, "{z}") && strings.Contains(str, "{x}") && strings.Contains(str, "{y}") {
		return true
	}
	if strings.ContainsAny(str, "{}") {
		return false
	}
	loc := strings.ToLower(strings.SplitN(strings.SplitN(str, "?", 2)[0], "#", 2)[0])
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp", ".mvt", ".pbf"} {
		if strings.HasSuffix(loc, ext) {
			return false
		}
	}
	pu, err := url.Parse(str)
	if err != nil {
		return false
	}
	if pu.Scheme == "http" || pu.Scheme == "https" {
		return pu.Host != ""
	}
	return pu.Scheme == "" && pu.Host == "" && strings.HasPrefix(pu.Path, "/")
}

func (s *Server) settingsUpdate(w http.ResponseWriter, r *http.Request, u *store.User) {
	body, err := httpx.DecodeBody(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	in, _ := body["settings"].(map[string]any)
	patch := map[string]any{}
	for k, v := range in {
		if settingKeys[k] {
			patch[k] = v
		}
	}
	if v, ok := patch["maps_maplibre_tiles_url"]; ok {
		if str, isStr := v.(string); isStr {
			if str = strings.TrimSpace(str); str == "" {
				patch["maps_maplibre_tiles_url"] = nil
			} else {
				patch["maps_maplibre_tiles_url"] = str
			}
		}
		if !validTilesURL(patch["maps_maplibre_tiles_url"]) {
			httpx.JSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "Something went wrong", "errors": []string{"Tile URL must contain {z}/{x}/{y} placeholders or be a style document URL"}})
			return
		}
	}
	if m, ok := patch["maps"].(map[string]any); ok {
		if du, _ := m["distance_unit"].(string); du != "km" && du != "mi" {
			delete(m, "distance_unit")
		}
		merged := map[string]any{}
		if cur, ok := u.Settings["maps"].(map[string]any); ok {
			for k, v := range cur {
				merged[k] = v
			}
		}
		for k, v := range m {
			merged[k] = v
		}
		patch["maps"] = merged
	}
	if v, ok := patch["minutes_between_routes"]; ok {
		if n := u.SettingInt("", 0); n == 0 {
			_ = v
		}
	}
	if err := s.S.MergeSettings(r.Context(), u.ID, patch); err != nil {
		httpx.JSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "Something went wrong", "errors": []string{err.Error()}})
		return
	}
	u2, err := s.S.UserByID(r.Context(), u.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	recalc := false
	for _, k := range []string{"minutes_between_routes", "meters_between_routes", "timezone"} {
		if _, ok := patch[k]; ok {
			recalc = true
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"message": "Settings updated", "settings": u2.EffectiveSettings(), "status": "success",
		"recalculation_triggered": recalc})
}

func (s *Server) statsIndex(w http.ResponseWriter, r *http.Request, u *store.User) {
	sum, err := stats.BuildSummary(r.Context(), s.S, u)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, sum)
}

func areaInput(body map[string]any) (name string, lat, lon float64, radius int, ok bool) {
	m, _ := body["area"].(map[string]any)
	if m == nil {
		m = body
	}
	name, _ = m["name"].(string)
	lat, ok1 := httpx.Float(m["latitude"])
	lon, ok2 := httpx.Float(m["longitude"])
	rf, ok3 := httpx.Float(m["radius"])
	return name, lat, lon, int(rf), name != "" && ok1 && ok2 && ok3 && rf > 0
}

func (s *Server) areasIndex(w http.ResponseWriter, r *http.Request, u *store.User) {
	a, err := s.S.ListAreas(r.Context(), u.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, a)
}

func (s *Server) areasShow(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	a, err := s.S.GetArea(r.Context(), u.ID, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, a)
}

func (s *Server) areasCreate(w http.ResponseWriter, r *http.Request, u *store.User) {
	body, err := httpx.DecodeBody(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	name, lat, lon, radius, ok := areaInput(body)
	if !ok {
		httpx.Errors(w, http.StatusUnprocessableEntity, "Name, latitude, longitude and radius are required")
		return
	}
	a, err := s.S.CreateArea(r.Context(), u.ID, name, lat, lon, radius)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, a)
}

func (s *Server) areasUpdate(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	body, err := httpx.DecodeBody(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	cur, err := s.S.GetArea(r.Context(), u.ID, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	m, _ := body["area"].(map[string]any)
	if m == nil {
		m = body
	}
	name, lat, lon, radius := cur.Name, cur.Latitude, cur.Longitude, cur.Radius
	if v, ok := m["name"].(string); ok && v != "" {
		name = v
	}
	if v, ok := httpx.Float(m["latitude"]); ok {
		lat = v
	}
	if v, ok := httpx.Float(m["longitude"]); ok {
		lon = v
	}
	if v, ok := httpx.Float(m["radius"]); ok && v > 0 {
		radius = int(v)
	}
	a, err := s.S.UpdateArea(r.Context(), u.ID, id, name, lat, lon, radius)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, a)
}

func (s *Server) areasDestroy(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	if err := s.S.DeleteArea(r.Context(), u.ID, id); err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"message": "Area was successfully deleted"})
}

var _ = json.Marshal
