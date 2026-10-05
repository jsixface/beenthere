package store

import "os"

// DefaultSettings mirrors Users::SafeSettings::DEFAULT_VALUES (the subset
// exposed by the API / used by the Go server).
func DefaultSettings() map[string]any {
	tz := os.Getenv("TIME_ZONE")
	if tz == "" {
		tz = "UTC"
	}
	return map[string]any{
		"fog_of_war_meters":            50,
		"fog_of_war_threshold":         50,
		"fog_of_war_mode":              "points",
		"meters_between_routes":        500,
		"preferred_map_layer":          "OpenStreetMap",
		"speed_colored_routes":         false,
		"points_rendering_mode":        "raw",
		"minutes_between_routes":       30,
		"time_threshold_minutes":       30,
		"merge_threshold_minutes":      15,
		"live_map_enabled":             true,
		"route_opacity":                0.6,
		"route_color":                  "#0000ff",
		"track_color":                  "#6366F1",
		"immich_url":                   nil,
		"immich_api_key":               nil,
		"photoprism_url":               nil,
		"photoprism_api_key":           nil,
		"maps":                         map[string]any{"distance_unit": "km"},
		"visits_suggestions_enabled":   true,
		"enabled_map_layers":           []string{"Tracks", "Heatmap"},
		"maps_maplibre_style":          "light",
		"maps_maplibre_tiles_url":      nil,
		"maps_maplibre_tiles_fallback": false,
		"globe_projection":             true,
		"min_minutes_spent_in_city":    60,
		"gps_filtering_enabled":        true,
		"timezone":                     tz,
		"visit_radius_meters":          100,
		"visit_min_points":             3,
		"visit_min_duration_minutes":   5,
		"point_dragging_enabled":       false,
		"points_tiled_rendering":       true,
		"places_tag_filters":           []string{},
		"enabled_transportation_modes": []string{},
		"speed_color_scale":            nil,
	}
}

// EffectiveSettings overlays the user's stored settings on the defaults.
// Values stored as numeric strings (Rails stores form input as strings) are
// kept as-is; callers coerce with SettingInt.
func (u *User) EffectiveSettings() map[string]any {
	out := DefaultSettings()
	for k, v := range u.Settings {
		if k == "maps" {
			if m, ok := v.(map[string]any); ok {
				merged := out["maps"].(map[string]any)
				for mk, mv := range m {
					merged[mk] = mv
				}
				continue
			}
		}
		out[k] = v
	}
	return out
}

// Setting returns an effective setting integer, falling back to the default.
func (u *User) Setting(key string) int {
	def := 0
	if d, ok := DefaultSettings()[key].(int); ok {
		def = d
	}
	return u.SettingInt(key, def)
}
