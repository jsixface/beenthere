# Porting notes and known differences

Everything here was verified against PostGIS 17 with the schema created by the bundled migration, **not** against a real
production Dawarich database or the Rails test-suite.

## Database compatibility
- Reads and writes the legacy `points` columns. Rows written by Rails after its `point_sources` migration are read
  correctly (Rails dual-writes the legacy columns); rows written by Beenthere are "unstamped" (`source_id` NULL), which
  Rails also supports. If a future Rails migration drops the legacy columns, `internal/store/points.go` must change.
- Beenthere creates the schema only in an empty database (`internal/migrate/sql`, generated from `db/schema.rb` at
  version 20260923180000, with `schema_migrations` populated so Rails can adopt it). It never alters an existing
  database; for later Dawarich schema versions, run Rails migrations with Rails. Constraint names differ from Rails'
  `fk_rails_*` names.
- Imports are parsed from the upload stream and **not stored** in ActiveStorage, so Rails cannot re-process them.
  Exports are streamed, not stored in the `exports` table.

## Behaviour differences
- **Tracks**: split purely on the time gap (`minutes_between_routes`), per `tracker_id`. No boundary merging, orphan
  re-absorption, transportation-mode segments or elevation smoothing. `dominant_mode` is left at its default.
- **Stats**: daily distance sums consecutive-point legs within the time gap, computed in SQL. H3 hexagons and flight
  distance are not calculated. Cities need reverse geocoding configured.
- **Visits**: a simple sliding-centroid stay detector (`visit_radius_meters`, `visit_min_duration_minutes`,
  `visit_min_points`, `merge_threshold_minutes`) with area/place attribution. Rails' scoring (`confidence`) is not
  reproduced. Declined/deleted/confirmed visits are never overwritten.
- **Places search** only searches the user's own places; external provider search is not implemented.
- **Anomaly filtering** (`gps_filtering_enabled`) is not applied; existing `anomaly` flags set by Rails are respected.
- **Realtime**: no ActionCable. The map reloads on demand.
- **Authentication**: sessions are a signed cookie (HMAC, 14 days) invalidated by a password change. API keys are
  unchanged. No plan/entitlement checks (self-hosted behaviour).
- Trips have a JSON API that the Rails app lacks (it renders HTML only); descriptions are stored as plain text in
  `action_text_rich_texts`.
