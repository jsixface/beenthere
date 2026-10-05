-- Dawarich schema, generated from db/schema.rb (ActiveRecord::Schema[8.1] version 2026_09_23_180000).
-- Applied only to an empty database; existing Dawarich databases are left untouched.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "postgis";

CREATE TABLE "achievement_progresses" (
    "id" bigserial NOT NULL,
    "achievement_key" character varying NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "sharing_enabled" boolean DEFAULT false NOT NULL,
    "sharing_uuid" character varying,
    "state" jsonb DEFAULT '{}' NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_achievement_progresses_on_sharing_uuid" ON "achievement_progresses" ("sharing_uuid");
CREATE UNIQUE INDEX "index_achievement_progresses_on_user_id_and_achievement_key" ON "achievement_progresses" ("user_id", "achievement_key");

CREATE TABLE "achievement_unlock_events" (
    "id" bigserial NOT NULL,
    "claim_token" character varying,
    "claimed_at" timestamp(6) without time zone,
    "created_at" timestamp(6) without time zone NOT NULL,
    "key" character varying NOT NULL,
    "kind" character varying NOT NULL,
    "seen_at" timestamp(6) without time zone,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_achievement_unlock_events_pending" ON "achievement_unlock_events" ("user_id", "id") WHERE (seen_at IS NULL);
CREATE UNIQUE INDEX "index_achievement_unlock_events_on_user_kind_key" ON "achievement_unlock_events" ("user_id", "kind", "key");
CREATE INDEX "index_achievement_unlock_events_on_user_id" ON "achievement_unlock_events" ("user_id");

CREATE TABLE "action_text_rich_texts" (
    "id" bigserial NOT NULL,
    "body" text,
    "created_at" timestamp(6) without time zone NOT NULL,
    "name" character varying NOT NULL,
    "record_id" bigint NOT NULL,
    "record_type" character varying NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_action_text_rich_texts_uniqueness" ON "action_text_rich_texts" ("record_type", "record_id", "name");

CREATE TABLE "active_storage_attachments" (
    "id" bigserial NOT NULL,
    "blob_id" bigint NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "name" character varying NOT NULL,
    "record_id" bigint NOT NULL,
    "record_type" character varying NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_active_storage_attachments_on_blob_id" ON "active_storage_attachments" ("blob_id");
CREATE UNIQUE INDEX "index_active_storage_attachments_uniqueness" ON "active_storage_attachments" ("record_type", "record_id", "name", "blob_id");

CREATE TABLE "active_storage_blobs" (
    "id" bigserial NOT NULL,
    "byte_size" bigint NOT NULL,
    "checksum" character varying,
    "content_type" character varying,
    "created_at" timestamp(6) without time zone NOT NULL,
    "filename" character varying NOT NULL,
    "key" character varying NOT NULL,
    "metadata" text,
    "service_name" character varying NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_active_storage_blobs_on_key" ON "active_storage_blobs" ("key");

CREATE TABLE "active_storage_variant_records" (
    "id" bigserial NOT NULL,
    "blob_id" bigint NOT NULL,
    "variation_digest" character varying NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_active_storage_variant_records_uniqueness" ON "active_storage_variant_records" ("blob_id", "variation_digest");

CREATE TABLE "areas" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "latitude" numeric(10,6) NOT NULL,
    "longitude" numeric(10,6) NOT NULL,
    "name" character varying NOT NULL,
    "radius" integer NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_areas_on_user_id" ON "areas" ("user_id");

CREATE TABLE "countries" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "geom" geometry(MultiPolygon,4326),
    "iso_a2" character varying NOT NULL,
    "iso_a3" character varying NOT NULL,
    "name" character varying NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_countries_on_geom" ON "countries" USING gist ("geom");
CREATE INDEX "index_countries_on_iso_a2" ON "countries" ("iso_a2");
CREATE INDEX "index_countries_on_iso_a3" ON "countries" ("iso_a3");
CREATE INDEX "index_countries_on_name" ON "countries" ("name");

CREATE TABLE "data_migrations" (
    "version" character varying NOT NULL,
    PRIMARY KEY ("version")
);

CREATE TABLE "digests" (
    "id" bigserial NOT NULL,
    "all_time_stats" jsonb DEFAULT '{}',
    "created_at" timestamp(6) without time zone NOT NULL,
    "distance" bigint DEFAULT 0 NOT NULL,
    "first_time_visits" jsonb DEFAULT '{}',
    "flight_distance" bigint DEFAULT 0 NOT NULL,
    "month" integer,
    "monthly_distances" jsonb DEFAULT '{}',
    "period_type" integer DEFAULT 0 NOT NULL,
    "sent_at" timestamp(6) without time zone,
    "sharing_settings" jsonb DEFAULT '{}',
    "sharing_uuid" uuid,
    "time_spent_by_location" jsonb DEFAULT '{}',
    "toponyms" jsonb DEFAULT '{}',
    "travel_patterns" jsonb DEFAULT '{}',
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    "year" integer NOT NULL,
    "year_over_year" jsonb DEFAULT '{}',
    PRIMARY KEY ("id")
);
CREATE INDEX "index_digests_on_period_type" ON "digests" ("period_type");
CREATE UNIQUE INDEX "index_digests_on_sharing_uuid" ON "digests" ("sharing_uuid");
CREATE UNIQUE INDEX "index_digests_on_user_year_month_period_type" ON "digests" ("user_id", "year", "month", "period_type");
CREATE UNIQUE INDEX "index_digests_on_user_year_period_type_monthless" ON "digests" ("user_id", "year", "period_type") WHERE (month IS NULL);
CREATE INDEX "index_digests_on_user_id" ON "digests" ("user_id");
CREATE INDEX "index_digests_on_year" ON "digests" ("year");

CREATE TABLE "exports" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "end_at" timestamp(6) without time zone,
    "error_message" text,
    "file_format" integer DEFAULT 0,
    "file_type" integer DEFAULT 0 NOT NULL,
    "name" character varying NOT NULL,
    "processing_started_at" timestamp(6) without time zone,
    "start_at" timestamp(6) without time zone,
    "status" integer DEFAULT 0 NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "url" character varying,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_exports_on_file_type" ON "exports" ("file_type");
CREATE INDEX "index_exports_on_status" ON "exports" ("status");
CREATE INDEX "index_exports_on_user_id" ON "exports" ("user_id");

CREATE TABLE "families" (
    "id" bigserial NOT NULL,
    "access_until" timestamp(6) without time zone,
    "created_at" timestamp(6) without time zone NOT NULL,
    "creator_id" bigint NOT NULL,
    "name" character varying(50) NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_families_on_creator_id" ON "families" ("creator_id");

CREATE TABLE "family_invitations" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "email" character varying NOT NULL,
    "expires_at" timestamp(6) without time zone NOT NULL,
    "family_id" bigint NOT NULL,
    "invited_by_id" bigint NOT NULL,
    "status" integer DEFAULT 0 NOT NULL,
    "token" character varying NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_family_invitations_on_family_id_and_email" ON "family_invitations" ("family_id", "email");
CREATE INDEX "index_family_invitations_on_family_status_expires" ON "family_invitations" ("family_id", "status", "expires_at");
CREATE INDEX "index_family_invitations_on_status_and_expires_at" ON "family_invitations" ("status", "expires_at");
CREATE INDEX "index_family_invitations_on_status_and_updated_at" ON "family_invitations" ("status", "updated_at");
CREATE UNIQUE INDEX "index_family_invitations_on_token" ON "family_invitations" ("token");

CREATE TABLE "family_location_requests" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "expires_at" timestamp(6) without time zone NOT NULL,
    "family_id" bigint NOT NULL,
    "requester_id" bigint NOT NULL,
    "responded_at" timestamp(6) without time zone,
    "status" integer DEFAULT 0 NOT NULL,
    "suggested_duration" character varying DEFAULT '24h' NOT NULL,
    "target_user_id" bigint NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "idx_family_loc_requests_expires_status" ON "family_location_requests" ("expires_at", "status");
CREATE INDEX "index_family_location_requests_on_family_id" ON "family_location_requests" ("family_id");
CREATE INDEX "idx_family_loc_requests_requester_target_status" ON "family_location_requests" ("requester_id", "target_user_id", "status");
CREATE INDEX "idx_family_loc_requests_target_status" ON "family_location_requests" ("target_user_id", "status");

CREATE TABLE "family_memberships" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "family_id" bigint NOT NULL,
    "role" integer DEFAULT 1 NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_family_memberships_on_family_and_role" ON "family_memberships" ("family_id", "role");
CREATE UNIQUE INDEX "index_family_memberships_on_user_id" ON "family_memberships" ("user_id");

CREATE TABLE "flights" (
    "id" bigserial NOT NULL,
    "aircraft_name" character varying,
    "aircraft_reg" character varying,
    "airline_iata" character varying,
    "airline_name" character varying,
    "arrival_time" timestamp(6) without time zone,
    "created_at" timestamp(6) without time zone NOT NULL,
    "date_precision" character varying DEFAULT 'day' NOT NULL,
    "departure_time" timestamp(6) without time zone,
    "distance_km" double precision,
    "external_id" integer NOT NULL,
    "flight_date" date,
    "flight_number" character varying,
    "from_code" character varying,
    "from_lat" double precision,
    "from_lon" double precision,
    "from_name" character varying,
    "note" text,
    "raw" jsonb DEFAULT '{}' NOT NULL,
    "seat" character varying,
    "seat_class" character varying,
    "to_code" character varying,
    "to_lat" double precision,
    "to_lon" double precision,
    "to_name" character varying,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_flights_on_user_id_and_departure_time" ON "flights" ("user_id", "departure_time");
CREATE UNIQUE INDEX "index_flights_on_user_id_and_external_id" ON "flights" ("user_id", "external_id");
CREATE INDEX "index_flights_on_user_id" ON "flights" ("user_id");

CREATE TABLE "flipper_features" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "key" character varying NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_flipper_features_on_key" ON "flipper_features" ("key");

CREATE TABLE "flipper_gates" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "feature_key" character varying NOT NULL,
    "key" character varying NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "value" text,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_flipper_gates_on_feature_key_and_key_and_value" ON "flipper_gates" ("feature_key", "key", "value");

CREATE TABLE "imports" (
    "id" bigserial NOT NULL,
    "additional_data_extraction" jsonb DEFAULT '{}' NOT NULL,
    "additional_data_extraction_status" integer DEFAULT 0 NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "demo" boolean DEFAULT false NOT NULL,
    "doubles" integer DEFAULT 0,
    "error_message" text,
    "name" character varying NOT NULL,
    "points_count" integer DEFAULT 0,
    "processed" integer DEFAULT 0,
    "processing_started_at" timestamp(6) without time zone,
    "raw_data" jsonb,
    "raw_points" integer DEFAULT 0,
    "source" integer,
    "status" integer DEFAULT 0 NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_imports_on_additional_data_extraction_status" ON "imports" ("additional_data_extraction_status");
CREATE INDEX "index_imports_on_source" ON "imports" ("source");
CREATE INDEX "index_imports_on_status" ON "imports" ("status");
CREATE INDEX "index_imports_on_user_id" ON "imports" ("user_id");

CREATE TABLE "instance_settings" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "encrypted_value" text,
    "key" character varying NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "value" jsonb,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_instance_settings_on_key" ON "instance_settings" ("key");

CREATE TABLE "notes" (
    "id" bigserial NOT NULL,
    "attachable_id" bigint,
    "attachable_type" character varying,
    "body" text,
    "created_at" timestamp(6) without time zone NOT NULL,
    "lonlat" geography(Point,4326),
    "noted_at" timestamp(6) without time zone,
    "source_digest" character varying,
    "title" character varying,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_notes_on_attachable_and_noted_date" ON "notes" (attachable_type, attachable_id, ((noted_at)::date)) WHERE (attachable_id IS NOT NULL);
CREATE INDEX "index_notes_on_attachable_type_and_attachable_id" ON "notes" ("attachable_type", "attachable_id");
CREATE INDEX "index_notes_on_lonlat" ON "notes" USING gist ("lonlat");
CREATE INDEX "index_notes_on_user_id_and_noted_at" ON "notes" ("user_id", "noted_at");
CREATE INDEX "index_notes_on_user_id" ON "notes" ("user_id");

CREATE TABLE "notifications" (
    "id" bigserial NOT NULL,
    "content" text NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "kind" integer DEFAULT 0 NOT NULL,
    "read_at" timestamp(6) without time zone,
    "title" character varying NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_notifications_on_kind" ON "notifications" ("kind");
CREATE INDEX "index_notifications_on_user_id" ON "notifications" ("user_id");

CREATE TABLE "pending_imports" (
    "id" bigserial NOT NULL,
    "claim_ticket" uuid DEFAULT gen_random_uuid() NOT NULL,
    "claimed_at" timestamp(6) without time zone,
    "claimed_by_user_id" bigint,
    "created_at" timestamp(6) without time zone NOT NULL,
    "expires_at" timestamp(6) without time zone NOT NULL,
    "origin" character varying NOT NULL,
    "original_filename" character varying NOT NULL,
    "source_hint" character varying,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_pending_imports_on_claim_ticket" ON "pending_imports" ("claim_ticket");
CREATE INDEX "index_pending_imports_on_claimed_by_user_id" ON "pending_imports" ("claimed_by_user_id");
CREATE INDEX "index_pending_imports_on_expires_at" ON "pending_imports" ("expires_at");

CREATE TABLE "place_visits" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "place_id" bigint NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "visit_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_place_visits_on_place_id" ON "place_visits" ("place_id");
CREATE UNIQUE INDEX "idx_place_visits_visit_id_place_id" ON "place_visits" ("visit_id", "place_id");

CREATE TABLE "places" (
    "id" bigserial NOT NULL,
    "city" character varying,
    "country" character varying,
    "created_at" timestamp(6) without time zone NOT NULL,
    "demo" boolean DEFAULT false NOT NULL,
    "geodata" jsonb DEFAULT '{}' NOT NULL,
    "import_id" bigint,
    "latitude" numeric(10,6) NOT NULL,
    "longitude" numeric(10,6) NOT NULL,
    "lonlat" geography(Point,4326),
    "name" character varying NOT NULL,
    "name_locked_at" timestamp(6) without time zone,
    "note" text,
    "reverse_geocoded_at" timestamp(6) without time zone,
    "source" integer DEFAULT 0,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_places_on_geodata_osm_id" ON "places" ((((geodata -> 'properties'::text) ->> 'osm_id'::text)));
CREATE UNIQUE INDEX "idx_places_user_external_place_id" ON "places" (user_id, ((geodata ->> 'external_place_id'::text))) WHERE ((geodata ->> 'external_place_id'::text) IS NOT NULL);
CREATE INDEX "index_places_on_demo_true" ON "places" ("demo") WHERE (demo = true);
CREATE INDEX "idx_places_import_id_extracted" ON "places" ("import_id") WHERE (import_id IS NOT NULL);
CREATE INDEX "index_places_on_lonlat" ON "places" USING gist ("lonlat");
CREATE INDEX "index_places_on_user_id" ON "places" ("user_id");

CREATE TABLE "planned_accommodations" (
    "id" bigserial NOT NULL,
    "address" character varying,
    "check_in_at" character varying,
    "check_out_at" character varying,
    "created_at" timestamp(6) without time zone NOT NULL,
    "ends_on" date,
    "latitude" numeric(10,6),
    "longitude" numeric(10,6),
    "name" character varying NOT NULL,
    "notes" text,
    "starts_on" date,
    "trip_id" bigint NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_planned_accommodations_on_trip_id" ON "planned_accommodations" ("trip_id");

CREATE TABLE "planned_day_notes" (
    "id" bigserial NOT NULL,
    "body" text NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "noted_at" character varying,
    "planned_day_id" bigint NOT NULL,
    "position" integer NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_planned_day_notes_on_planned_day_id_and_position" ON "planned_day_notes" ("planned_day_id", "position");
CREATE INDEX "index_planned_day_notes_on_planned_day_id" ON "planned_day_notes" ("planned_day_id");

CREATE TABLE "planned_days" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "date" date NOT NULL,
    "notes" text,
    "position" integer NOT NULL,
    "title" character varying,
    "trip_id" bigint NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_planned_days_on_trip_id_and_date" ON "planned_days" ("trip_id", "date");
CREATE INDEX "index_planned_days_on_trip_id" ON "planned_days" ("trip_id");

CREATE TABLE "planned_reservations" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "ends_at" timestamp(6) without time zone,
    "location" character varying,
    "notes" text,
    "planned_day_id" bigint,
    "reservation_type" character varying,
    "starts_at" timestamp(6) without time zone,
    "status" character varying,
    "title" character varying NOT NULL,
    "trip_id" bigint NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_planned_reservations_on_planned_day_id" ON "planned_reservations" ("planned_day_id");
CREATE INDEX "index_planned_reservations_on_trip_id" ON "planned_reservations" ("trip_id");

CREATE TABLE "planned_stops" (
    "id" bigserial NOT NULL,
    "address" character varying,
    "category" character varying,
    "created_at" timestamp(6) without time zone NOT NULL,
    "duration_minutes" integer,
    "ends_at" character varying,
    "latitude" numeric(10,6),
    "longitude" numeric(10,6),
    "name" character varying NOT NULL,
    "notes" text,
    "planned_day_id" bigint NOT NULL,
    "position" integer NOT NULL,
    "starts_at" character varying,
    "transport_mode" character varying,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_planned_stops_on_planned_day_id_and_position" ON "planned_stops" ("planned_day_id", "position");
CREATE INDEX "index_planned_stops_on_planned_day_id" ON "planned_stops" ("planned_day_id");

CREATE TABLE "planned_travellers" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "name" character varying NOT NULL,
    "owner" boolean DEFAULT false NOT NULL,
    "trip_id" bigint NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_planned_travellers_on_trip_id" ON "planned_travellers" ("trip_id");

CREATE TABLE "planned_unplanned_places" (
    "id" bigserial NOT NULL,
    "address" character varying,
    "category" character varying,
    "created_at" timestamp(6) without time zone NOT NULL,
    "duration_minutes" integer,
    "ends_at" character varying,
    "latitude" numeric(10,6),
    "longitude" numeric(10,6),
    "name" character varying NOT NULL,
    "notes" text,
    "position" integer NOT NULL,
    "starts_at" character varying,
    "transport_mode" character varying,
    "trip_id" bigint NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_planned_unplanned_places_on_trip_id_and_position" ON "planned_unplanned_places" ("trip_id", "position");
CREATE INDEX "index_planned_unplanned_places_on_trip_id" ON "planned_unplanned_places" ("trip_id");

CREATE TABLE "point_sources" (
    "id" serial NOT NULL,
    "battery_status" integer,
    "bssid" character varying,
    "connection" integer,
    "created_at" timestamp(6) without time zone NOT NULL,
    "digest" character varying(32) NOT NULL,
    "in_regions" text[],
    "inrids" text[],
    "ssid" character varying,
    "topic" character varying,
    "tracker_id" character varying,
    "trigger" integer,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_point_sources_on_digest" ON "point_sources" ("digest");

CREATE TABLE "points" (
    "id" bigserial NOT NULL,
    "accuracy" integer,
    "altitude" integer,
    "altitude_decimal" numeric(10,2),
    "anomaly" boolean,
    "battery" integer,
    "battery_status" integer,
    "bssid" character varying,
    "city" character varying,
    "connection" integer,
    "country" character varying,
    "country_id" bigint,
    "country_name" character varying,
    "course" numeric(8,5),
    "course_accuracy" numeric(8,5),
    "created_at" timestamp(6) without time zone NOT NULL,
    "external_track_id" character varying,
    "geodata" jsonb DEFAULT '{}' NOT NULL,
    "import_id" bigint,
    "in_regions" text[] DEFAULT '{}',
    "inrids" text[] DEFAULT '{}',
    "lock_version" integer DEFAULT 0 NOT NULL,
    "lonlat" geography(Point,4326),
    "mode" integer,
    "motion_data" jsonb DEFAULT '{}' NOT NULL,
    "ping" character varying,
    "raw_data" jsonb DEFAULT '{}',
    "raw_data_archive_id" bigint,
    "raw_data_archived" boolean DEFAULT false NOT NULL,
    "reverse_geocoded_at" timestamp(6) without time zone,
    "source_id" integer,
    "ssid" character varying,
    "timestamp" integer,
    "topic" character varying,
    "track_id" bigint,
    "tracker_id" character varying,
    "trigger" integer,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint,
    "velocity" character varying,
    "vertical_accuracy" integer,
    "visit_id" bigint,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_points_on_not_reverse_geocoded" ON "points" ("id") WHERE (reverse_geocoded_at IS NULL);
CREATE INDEX "index_points_on_import_id" ON "points" ("import_id");
CREATE INDEX "index_points_on_lonlat" ON "points" USING gist ("lonlat");
CREATE INDEX "index_points_on_raw_data_archive_id" ON "points" ("raw_data_archive_id");
CREATE INDEX "idx_points_track_id_timestamp" ON "points" ("track_id", "timestamp");
CREATE INDEX "index_points_on_user_id_and_created_at" ON "points" ("user_id", "created_at");
CREATE INDEX "index_points_on_unarchived" ON "points" ("user_id", "id") WHERE ((raw_data_archived = false) AND (raw_data <> '{}'::jsonb));
CREATE UNIQUE INDEX "index_points_on_user_id_timestamp_lonlat" ON "points" ("user_id", "timestamp", "lonlat");
CREATE INDEX "idx_points_user_id_legacy_tracker" ON "points" ("user_id") WHERE ((tracker_id)::text = ANY (ARRAY[('google-maps-timeline-export'::character varying)::text, ('google-maps-phone-timeline-export'::character varying)::text]));
CREATE INDEX "index_points_on_visit_id" ON "points" ("visit_id");

CREATE TABLE "points_raw_data_archives" (
    "id" bigserial NOT NULL,
    "archived_at" timestamp(6) without time zone NOT NULL,
    "chunk_number" integer DEFAULT 1 NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "metadata" jsonb DEFAULT '{}' NOT NULL,
    "month" integer NOT NULL,
    "point_count" integer NOT NULL,
    "point_ids_checksum" character varying NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    "verified_at" timestamp(6) without time zone,
    "year" integer NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_points_raw_data_archives_on_archived_at" ON "points_raw_data_archives" ("archived_at");
CREATE UNIQUE INDEX "index_raw_data_archives_uniqueness" ON "points_raw_data_archives" ("user_id", "year", "month", "chunk_number");
CREATE INDEX "index_points_raw_data_archives_on_user_id_and_year_and_month" ON "points_raw_data_archives" ("user_id", "year", "month");
CREATE INDEX "index_points_raw_data_archives_on_user_id" ON "points_raw_data_archives" ("user_id");

CREATE TABLE "posters" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "name" character varying NOT NULL,
    "settings" jsonb DEFAULT '{}' NOT NULL,
    "status" integer DEFAULT 0 NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_posters_on_user_id" ON "posters" ("user_id");

CREATE TABLE "regions" (
    "id" bigserial NOT NULL,
    "code" character varying NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "geom" geometry(MultiPolygon,4326) NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_regions_on_code" ON "regions" ("code");
CREATE INDEX "index_regions_on_geom" ON "regions" USING gist ("geom");

CREATE TABLE "route_videos" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "expired_at" timestamp(6) without time zone,
    "name" character varying NOT NULL,
    "settings" jsonb DEFAULT '{}' NOT NULL,
    "status" integer DEFAULT 0 NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_route_videos_on_user_id_and_created_at" ON "route_videos" ("user_id", "created_at");

CREATE TABLE "service_settings" (
    "id" bigserial NOT NULL,
    "active" boolean DEFAULT false NOT NULL,
    "config" jsonb DEFAULT '{}' NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "credentials" text,
    "provider" character varying NOT NULL,
    "service" integer NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_service_settings_on_user_id_and_service_and_provider" ON "service_settings" ("user_id", "service", "provider");
CREATE UNIQUE INDEX "index_service_settings_on_user_service_active" ON "service_settings" ("user_id", "service") WHERE active;
CREATE INDEX "index_service_settings_on_user_id" ON "service_settings" ("user_id");

CREATE TABLE "shared_links" (
    "id" uuid NOT NULL DEFAULT gen_random_uuid(),
    "created_at" timestamp(6) without time zone NOT NULL,
    "expires_at" timestamp(6) without time zone,
    "last_accessed_at" timestamp(6) without time zone,
    "magic_phrase" character varying(255),
    "name" character varying(255) NOT NULL,
    "resource_id" bigint,
    "resource_type" integer NOT NULL,
    "revoked_at" timestamp(6) without time zone,
    "settings" jsonb DEFAULT '{}' NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    "view_count" integer DEFAULT 0 NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_shared_links_on_resource_type_and_resource_id" ON "shared_links" ("resource_type", "resource_id") WHERE (resource_id IS NOT NULL);
CREATE INDEX "index_shared_links_active_by_user" ON "shared_links" ("user_id") WHERE (revoked_at IS NULL);
CREATE INDEX "index_shared_links_on_user_id" ON "shared_links" ("user_id");

CREATE TABLE "stats" (
    "id" bigserial NOT NULL,
    "calculation_version" integer DEFAULT 0 NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "daily_distance" jsonb DEFAULT '{}',
    "distance" bigint NOT NULL,
    "flight_distance" bigint DEFAULT 0 NOT NULL,
    "h3_hex_ids" jsonb DEFAULT '{}',
    "month" integer NOT NULL,
    "repair_deferred_at" timestamp(6) without time zone,
    "sharing_settings" jsonb DEFAULT '{}',
    "sharing_uuid" uuid,
    "toponyms" jsonb,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    "year" integer NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_stats_on_distance" ON "stats" ("distance");
CREATE INDEX "index_stats_on_h3_hex_ids" ON "stats" USING gin ("h3_hex_ids") WHERE ((h3_hex_ids IS NOT NULL) AND (h3_hex_ids <> '{}'::jsonb));
CREATE INDEX "index_stats_on_month" ON "stats" ("month");
CREATE UNIQUE INDEX "index_stats_on_sharing_uuid" ON "stats" ("sharing_uuid");
CREATE UNIQUE INDEX "index_stats_on_user_id_year_month" ON "stats" ("user_id", "year", "month");
CREATE INDEX "index_stats_on_user_id" ON "stats" ("user_id");
CREATE INDEX "index_stats_on_year" ON "stats" ("year");

CREATE TABLE "taggings" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "tag_id" bigint NOT NULL,
    "taggable_id" bigint NOT NULL,
    "taggable_type" character varying NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_taggings_on_tag_id" ON "taggings" ("tag_id");
CREATE UNIQUE INDEX "index_taggings_on_taggable_and_tag" ON "taggings" ("taggable_type", "taggable_id", "tag_id");
CREATE INDEX "index_taggings_on_taggable" ON "taggings" ("taggable_type", "taggable_id");

CREATE TABLE "tags" (
    "id" bigserial NOT NULL,
    "color" character varying,
    "created_at" timestamp(6) without time zone NOT NULL,
    "demo" boolean DEFAULT false NOT NULL,
    "icon" character varying,
    "name" character varying NOT NULL,
    "privacy_radius_meters" integer,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_tags_on_demo_true" ON "tags" ("demo") WHERE (demo = true);
CREATE INDEX "index_tags_on_privacy_radius_meters" ON "tags" ("privacy_radius_meters") WHERE (privacy_radius_meters IS NOT NULL);
CREATE UNIQUE INDEX "index_tags_on_user_id_and_name" ON "tags" ("user_id", "name");
CREATE INDEX "index_tags_on_user_id" ON "tags" ("user_id");

CREATE TABLE "track_segments" (
    "id" bigserial NOT NULL,
    "avg_acceleration" double precision,
    "avg_speed" double precision,
    "confidence" integer DEFAULT 0,
    "confidence_score" double precision,
    "corrected_at" timestamp(6) without time zone,
    "created_at" timestamp(6) without time zone NOT NULL,
    "distance" integer,
    "duration" integer,
    "end_at" timestamp with time zone,
    "end_index" integer,
    "max_speed" double precision,
    "path" geometry(LineString,4326),
    "source" character varying,
    "start_at" timestamp with time zone,
    "start_index" integer,
    "track_id" bigint NOT NULL,
    "transportation_mode" integer DEFAULT 0 NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_track_segments_on_corrected_at" ON "track_segments" ("corrected_at") WHERE (corrected_at IS NOT NULL);
CREATE UNIQUE INDEX "idx_track_segments_track_start_at_unique" ON "track_segments" ("track_id", "start_at") WHERE (start_at IS NOT NULL);
CREATE INDEX "index_track_segments_on_track_and_indices" ON "track_segments" ("track_id", "start_index", "end_index");
CREATE UNIQUE INDEX "idx_track_segments_track_start_index_unique" ON "track_segments" ("track_id", "start_index");
CREATE INDEX "index_track_segments_on_track_id_and_transportation_mode" ON "track_segments" ("track_id", "transportation_mode");

CREATE TABLE "tracks" (
    "id" bigserial NOT NULL,
    "avg_speed" double precision,
    "created_at" timestamp(6) without time zone NOT NULL,
    "demo" boolean DEFAULT false NOT NULL,
    "distance" bigint,
    "dominant_mode" integer DEFAULT 0,
    "duration" integer,
    "elevation_gain" integer,
    "elevation_loss" integer,
    "elevation_max" integer,
    "elevation_min" integer,
    "end_at" timestamp(6) without time zone NOT NULL,
    "import_id" bigint,
    "lock_version" integer DEFAULT 0 NOT NULL,
    "original_path" geometry(LineString,4326) NOT NULL,
    "start_at" timestamp(6) without time zone NOT NULL,
    "tracker_id" character varying,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_tracks_on_user_tracker_start_end_unique" ON "tracks" (user_id, COALESCE(tracker_id, ''::character varying), start_at, end_at);
CREATE INDEX "index_tracks_on_demo_true" ON "tracks" ("demo") WHERE (demo = true);
CREATE INDEX "index_tracks_on_dominant_mode" ON "tracks" ("dominant_mode");
CREATE INDEX "idx_tracks_import_id_extracted" ON "tracks" ("import_id") WHERE (import_id IS NOT NULL);
CREATE INDEX "index_tracks_on_original_path" ON "tracks" USING gist ("original_path");
CREATE INDEX "idx_tracks_user_id_start_at" ON "tracks" ("user_id", "start_at");
CREATE INDEX "idx_tracks_user_tracker_end_at" ON "tracks" ("user_id", "tracker_id", "end_at");
CREATE INDEX "index_tracks_on_user_id" ON "tracks" ("user_id");

CREATE TABLE "trip_sources" (
    "id" bigserial NOT NULL,
    "api_key" text,
    "base_url" character varying NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "importing" boolean DEFAULT false NOT NULL,
    "last_error" text,
    "last_synced_at" timestamp(6) without time zone,
    "provider" character varying NOT NULL,
    "selection_token" character varying,
    "status" integer DEFAULT 0 NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_trip_sources_on_user_id_and_provider_and_base_url" ON "trip_sources" ("user_id", "provider", "base_url");
CREATE INDEX "index_trip_sources_on_user_id" ON "trip_sources" ("user_id");

CREATE TABLE "trips" (
    "id" bigserial NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "demo" boolean DEFAULT false NOT NULL,
    "distance" bigint,
    "ended_at" timestamp(6) without time zone NOT NULL,
    "last_recalculated_at" timestamp(6) without time zone,
    "name" character varying NOT NULL,
    "path" geometry(LineString,4326),
    "source_digest" character varying,
    "source_identifier" character varying,
    "source_snapshot" jsonb DEFAULT '{}' NOT NULL,
    "source_status" integer,
    "source_synced_at" timestamp(6) without time zone,
    "started_at" timestamp(6) without time zone NOT NULL,
    "trip_source_id" bigint,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    "visited_countries" jsonb DEFAULT '{}' NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_trips_on_demo_true" ON "trips" ("demo") WHERE (demo = true);
CREATE UNIQUE INDEX "index_trips_on_source_identifier" ON "trips" ("trip_source_id", "source_identifier") WHERE ((trip_source_id IS NOT NULL) AND (source_identifier IS NOT NULL));
CREATE INDEX "index_trips_on_trip_source_id" ON "trips" ("trip_source_id");
CREATE INDEX "index_trips_on_user_id" ON "trips" ("user_id");

CREATE TABLE "user_achievements" (
    "id" bigserial NOT NULL,
    "achievement_key" character varying NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "earned_at" timestamp(6) without time zone NOT NULL,
    "metadata" jsonb DEFAULT '{}' NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "index_user_achievements_on_user_id_and_achievement_key" ON "user_achievements" ("user_id", "achievement_key");

CREATE TABLE "users" (
    "id" bigserial NOT NULL,
    "active_until" timestamp(6) without time zone,
    "admin" boolean DEFAULT false,
    "api_key" character varying DEFAULT '' NOT NULL,
    "changelog_consent" integer,
    "consumed_timestep" integer,
    "created_at" timestamp(6) without time zone NOT NULL,
    "current_sign_in_at" timestamp(6) without time zone,
    "current_sign_in_ip" character varying,
    "deleted_at" timestamp(6) without time zone,
    "email" character varying DEFAULT '' NOT NULL,
    "encrypted_password" character varying DEFAULT '' NOT NULL,
    "failed_attempts" integer DEFAULT 0 NOT NULL,
    "failed_otp_attempts" integer DEFAULT 0 NOT NULL,
    "first_name" character varying,
    "last_name" character varying,
    "last_sign_in_at" timestamp(6) without time zone,
    "last_sign_in_ip" character varying,
    "locked_at" timestamp(6) without time zone,
    "otp_backup_codes" text[],
    "otp_locked_at" timestamp(6) without time zone,
    "otp_required_for_login" boolean DEFAULT false NOT NULL,
    "otp_secret" character varying,
    "plan" integer DEFAULT 1 NOT NULL,
    "points_count" integer DEFAULT 0 NOT NULL,
    "provider" character varying,
    "remember_created_at" timestamp(6) without time zone,
    "reset_password_sent_at" timestamp(6) without time zone,
    "reset_password_token" character varying,
    "settings" jsonb DEFAULT '{"fog_of_war_meters":"100","meters_between_routes":"1000","minutes_between_routes":"60"}',
    "sign_in_count" integer DEFAULT 0 NOT NULL,
    "signup_variant" character varying,
    "stats_swept_at" timestamp(6) without time zone,
    "status" integer DEFAULT 0,
    "subscription_source" integer DEFAULT 0 NOT NULL,
    "theme" character varying DEFAULT 'dark' NOT NULL,
    "uid" character varying,
    "unlock_token" character varying,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "utm_campaign" character varying,
    "utm_content" character varying,
    "utm_medium" character varying,
    "utm_source" character varying,
    "utm_term" character varying,
    "visits_redetected_at" timestamp(6) without time zone DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_users_on_api_key" ON "users" ("api_key");
CREATE INDEX "index_users_on_deleted_at" ON "users" ("deleted_at");
CREATE UNIQUE INDEX "index_users_on_email" ON "users" ("email");
CREATE INDEX "index_users_on_otp_locked_at_not_null" ON "users" ("otp_locked_at") WHERE (otp_locked_at IS NOT NULL);
CREATE INDEX "index_users_on_plan" ON "users" ("plan");
CREATE UNIQUE INDEX "index_users_on_provider_and_uid_present" ON "users" ("provider", "uid") WHERE ((provider IS NOT NULL) AND (uid IS NOT NULL));
CREATE UNIQUE INDEX "index_users_on_reset_password_token" ON "users" ("reset_password_token");
CREATE INDEX "index_users_on_signup_variant_reverse_trial" ON "users" ("signup_variant") WHERE ((signup_variant)::text = 'reverse_trial'::text);
CREATE INDEX "index_users_on_status" ON "users" ("status");
CREATE UNIQUE INDEX "index_users_on_unlock_token" ON "users" ("unlock_token");
CREATE INDEX "index_users_on_visits_redetected_at" ON "users" ("visits_redetected_at");

CREATE TABLE "visits" (
    "id" bigserial NOT NULL,
    "area_id" bigint,
    "confidence" smallint,
    "confidence_breakdown" jsonb DEFAULT '{}' NOT NULL,
    "created_at" timestamp(6) without time zone NOT NULL,
    "deleted_at" timestamp(6) without time zone,
    "demo" boolean DEFAULT false NOT NULL,
    "detection_version" smallint,
    "duration" integer NOT NULL,
    "ended_at" timestamp(6) without time zone NOT NULL,
    "import_id" bigint,
    "name" character varying NOT NULL,
    "place_id" bigint,
    "started_at" timestamp(6) without time zone NOT NULL,
    "status" integer DEFAULT 0 NOT NULL,
    "updated_at" timestamp(6) without time zone NOT NULL,
    "user_id" bigint NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "index_visits_on_area_id" ON "visits" ("area_id");
CREATE INDEX "index_visits_on_demo_true" ON "visits" ("demo") WHERE (demo = true);
CREATE INDEX "idx_visits_import_id_extracted" ON "visits" ("import_id") WHERE (import_id IS NOT NULL);
CREATE INDEX "index_visits_on_place_id" ON "visits" ("place_id");
CREATE INDEX "index_visits_on_started_at" ON "visits" ("started_at");
CREATE UNIQUE INDEX "idx_visits_user_started_at_place_unique" ON "visits" ("user_id", "started_at", "place_id");
CREATE INDEX "index_visits_on_user_id" ON "visits" ("user_id");

ALTER TABLE "achievement_progresses" ADD CONSTRAINT "fk_achievement_progresses_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "achievement_unlock_events" ADD CONSTRAINT "fk_achievement_unlock_events_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE;
ALTER TABLE "active_storage_attachments" ADD CONSTRAINT "fk_active_storage_attachments_blob_id" FOREIGN KEY ("blob_id") REFERENCES "active_storage_blobs" ("id");
ALTER TABLE "active_storage_variant_records" ADD CONSTRAINT "fk_active_storage_variant_records_blob_id" FOREIGN KEY ("blob_id") REFERENCES "active_storage_blobs" ("id");
ALTER TABLE "areas" ADD CONSTRAINT "fk_areas_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "digests" ADD CONSTRAINT "fk_digests_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "families" ADD CONSTRAINT "fk_families_creator_id" FOREIGN KEY ("creator_id") REFERENCES "users" ("id");
ALTER TABLE "family_invitations" ADD CONSTRAINT "fk_family_invitations_family_id" FOREIGN KEY ("family_id") REFERENCES "families" ("id");
ALTER TABLE "family_invitations" ADD CONSTRAINT "fk_family_invitations_invited_by_id" FOREIGN KEY ("invited_by_id") REFERENCES "users" ("id");
ALTER TABLE "family_location_requests" ADD CONSTRAINT "fk_family_location_requests_family_id" FOREIGN KEY ("family_id") REFERENCES "families" ("id");
ALTER TABLE "family_location_requests" ADD CONSTRAINT "fk_family_location_requests_requester_id" FOREIGN KEY ("requester_id") REFERENCES "users" ("id");
ALTER TABLE "family_location_requests" ADD CONSTRAINT "fk_family_location_requests_target_user_id" FOREIGN KEY ("target_user_id") REFERENCES "users" ("id");
ALTER TABLE "family_memberships" ADD CONSTRAINT "fk_family_memberships_family_id" FOREIGN KEY ("family_id") REFERENCES "families" ("id");
ALTER TABLE "family_memberships" ADD CONSTRAINT "fk_family_memberships_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "flights" ADD CONSTRAINT "fk_flights_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "notes" ADD CONSTRAINT "fk_notes_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "notifications" ADD CONSTRAINT "fk_notifications_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "pending_imports" ADD CONSTRAINT "fk_pending_imports_claimed_by_user_id" FOREIGN KEY ("claimed_by_user_id") REFERENCES "users" ("id") ON DELETE SET NULL;
ALTER TABLE "place_visits" ADD CONSTRAINT "fk_place_visits_place_id" FOREIGN KEY ("place_id") REFERENCES "places" ("id");
ALTER TABLE "place_visits" ADD CONSTRAINT "fk_place_visits_visit_id" FOREIGN KEY ("visit_id") REFERENCES "visits" ("id");
ALTER TABLE "planned_accommodations" ADD CONSTRAINT "fk_planned_accommodations_trip_id" FOREIGN KEY ("trip_id") REFERENCES "trips" ("id");
ALTER TABLE "planned_day_notes" ADD CONSTRAINT "fk_planned_day_notes_planned_day_id" FOREIGN KEY ("planned_day_id") REFERENCES "planned_days" ("id");
ALTER TABLE "planned_days" ADD CONSTRAINT "fk_planned_days_trip_id" FOREIGN KEY ("trip_id") REFERENCES "trips" ("id");
ALTER TABLE "planned_reservations" ADD CONSTRAINT "fk_planned_reservations_planned_day_id" FOREIGN KEY ("planned_day_id") REFERENCES "planned_days" ("id");
ALTER TABLE "planned_reservations" ADD CONSTRAINT "fk_planned_reservations_trip_id" FOREIGN KEY ("trip_id") REFERENCES "trips" ("id");
ALTER TABLE "planned_stops" ADD CONSTRAINT "fk_planned_stops_planned_day_id" FOREIGN KEY ("planned_day_id") REFERENCES "planned_days" ("id");
ALTER TABLE "planned_travellers" ADD CONSTRAINT "fk_planned_travellers_trip_id" FOREIGN KEY ("trip_id") REFERENCES "trips" ("id");
ALTER TABLE "planned_unplanned_places" ADD CONSTRAINT "fk_planned_unplanned_places_trip_id" FOREIGN KEY ("trip_id") REFERENCES "trips" ("id");
ALTER TABLE "points" ADD CONSTRAINT "fk_points_raw_data_archive_id" FOREIGN KEY ("raw_data_archive_id") REFERENCES "points_raw_data_archives" ("id") ON DELETE RESTRICT;
ALTER TABLE "points" ADD CONSTRAINT "fk_points_track_id" FOREIGN KEY ("track_id") REFERENCES "tracks" ("id");
ALTER TABLE "points" ADD CONSTRAINT "fk_points_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "points" ADD CONSTRAINT "fk_points_visit_id" FOREIGN KEY ("visit_id") REFERENCES "visits" ("id");
ALTER TABLE "points_raw_data_archives" ADD CONSTRAINT "fk_points_raw_data_archives_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "posters" ADD CONSTRAINT "fk_posters_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "route_videos" ADD CONSTRAINT "fk_route_videos_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "service_settings" ADD CONSTRAINT "fk_service_settings_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "shared_links" ADD CONSTRAINT "fk_shared_links_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE;
ALTER TABLE "stats" ADD CONSTRAINT "fk_stats_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "taggings" ADD CONSTRAINT "fk_taggings_tag_id" FOREIGN KEY ("tag_id") REFERENCES "tags" ("id");
ALTER TABLE "tags" ADD CONSTRAINT "fk_tags_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "track_segments" ADD CONSTRAINT "fk_track_segments_track_id" FOREIGN KEY ("track_id") REFERENCES "tracks" ("id");
ALTER TABLE "tracks" ADD CONSTRAINT "fk_tracks_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "trip_sources" ADD CONSTRAINT "fk_trip_sources_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "trips" ADD CONSTRAINT "fk_trips_trip_source_id" FOREIGN KEY ("trip_source_id") REFERENCES "trip_sources" ("id");
ALTER TABLE "trips" ADD CONSTRAINT "fk_trips_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "user_achievements" ADD CONSTRAINT "fk_user_achievements_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "visits" ADD CONSTRAINT "fk_visits_area_id" FOREIGN KEY ("area_id") REFERENCES "areas" ("id");
ALTER TABLE "visits" ADD CONSTRAINT "fk_visits_place_id" FOREIGN KEY ("place_id") REFERENCES "places" ("id");
ALTER TABLE "visits" ADD CONSTRAINT "fk_visits_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id");
ALTER TABLE "users" ADD CONSTRAINT "users_admin_null" CHECK (admin IS NOT NULL) NOT VALID;

-- Rails bookkeeping so the Rails app can adopt this database later.
CREATE TABLE "schema_migrations" ("version" character varying NOT NULL PRIMARY KEY);
CREATE TABLE "ar_internal_metadata" ("key" character varying NOT NULL PRIMARY KEY, "value" character varying,
  "created_at" timestamp(6) without time zone NOT NULL, "updated_at" timestamp(6) without time zone NOT NULL);
INSERT INTO "schema_migrations" ("version") VALUES ('20260923180000');
INSERT INTO "ar_internal_metadata" ("key", "value", "created_at", "updated_at") VALUES ('environment', 'production', now(), now());

