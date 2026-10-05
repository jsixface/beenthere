# Beenthere

A Go port of [Dawarich](https://github.com/Freika/dawarich) (self-hosted location history), built to start fast and
use little memory. It is a single static binary (~15 MB stripped, ~16 MB RSS idle) that talks to the **same PostgreSQL/PostGIS
schema** as the Rails app, so you can point it at an existing Dawarich database and compare the two directly.
No Redis, no Sidekiq: background work runs in-process.

## Run

```bash
export DATABASE_URL=postgres://user:pass@localhost:5432/dawarich_production   # or DATABASE_HOST/NAME/USERNAME/PASSWORD
export SECRET_KEY_BASE=$(openssl rand -hex 32)                                # signs session cookies
go run ./cmd/beenthere                                                       # listens on :3000 (PORT)
```

On an **empty** database Beenthere creates the full Dawarich schema itself (bundled migration, Rails schema version
`20260923180000`, generated from Dawarich's `db/schema.rb`). A database that already has a `users` table is never
touched. Create the first user with:

```bash
beenthere adduser you@example.com --admin     # password from $BT_PASSWORD, or generated and printed once
```

Sign in at `/login` with an existing Dawarich account (Devise bcrypt hashes work as-is). Accounts with 2FA enabled are
refused for now. The API accepts `?api_key=` or `Authorization: Bearer <key>` exactly like Dawarich.

Optional: `PHOTON_API_HOST` / `NOMINATIM_API_HOST` (+ `*_USE_HTTPS`, `REVERSE_GEOCODING_RPS`) enable reverse geocoding
(city names for stats). Nothing external is contacted unless you set one. `WORKERS`, `DB_POOL`, `MAX_UPLOAD_MB` tune
resources.

## Docker

Images are published to `ghcr.io/jsixface/beenthere`: `testing` follows the `main` branch, and every git tag `vX.Y.Z`
publishes `X.Y.Z` and `latest` (amd64 and arm64).

```bash
docker run -d --name beenthere -p 3000:3000 \
  -e DATABASE_URL='postgres://user:pass@db-host:5432/beenthere?sslmode=disable' \
  -e SECRET_KEY_BASE="$(openssl rand -hex 32)" \
  ghcr.io/jsixface/beenthere:latest
docker exec beenthere /beenthere adduser you@example.com --admin
```

### Docker Compose

```yaml
services:
  db:
    image: postgis/postgis:17-3.5-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: beenthere
      POSTGRES_PASSWORD: change-me
      POSTGRES_DB: beenthere
    volumes:
      - db-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U beenthere"]
      interval: 5s
      retries: 10

  app:
    image: ghcr.io/jsixface/beenthere:latest
    restart: unless-stopped
    depends_on:
      db:
        condition: service_healthy
    ports:
      - "3000:3000"
    environment:
      DATABASE_URL: postgres://beenthere:change-me@db:5432/beenthere?sslmode=disable
      SECRET_KEY_BASE: replace-with-output-of-openssl-rand-hex-32
      # Optional reverse geocoding (city names in stats):
      # PHOTON_API_HOST: photon.example.com

volumes:
  db-data:
```

```bash
docker compose up -d
docker compose exec app /beenthere adduser you@example.com --admin   # prints a password and API key
```

The schema is created automatically on first start. Put a TLS-terminating reverse proxy in front for public use.

## What is ported

| Area | Status |
| --- | --- |
| Ingest: `POST /api/v1/points`, Overland, OwnTracks, Traccar | done |
| Points API: list (pagination, bbox, ETag, slim), update, delete, bulk delete, tracked months | done |
| Tracks: generation from points, list/show, track points, MVT tiles | done |
| Stats: monthly distance, countries/cities (toponyms), `/api/v1/stats` | done |
| Visits: detection (stay clustering), CRUD, merge, batch, bulk status | done |
| Places, tags (+ privacy zones), notes, areas | done |
| Trips (JSON API at `/api/v1/trips`, path/distance/countries recalculation) | done |
| Import: GPX, KML, GeoJSON, OwnTracks `.rec`, Google Records / phone Timeline | done |
| Export: GPX, GeoJSON (streamed) | done |
| Settings, `users/me`, health | done |
| Web UI: login, map (MapLibre, bundled), stats, visits, trips, places, imports, exports, settings | done (new, simplified) |

Not ported: families/sharing, achievements, posters/route videos, MCP, Immich/Photoprism/Polarsteps/Tesla integrations,
subscriptions, OIDC/OAuth/2FA sign-in, H3 hexagons / fog of war, FIT/TCX/CSV imports, raw-data archival, live
WebSocket map updates, and the Rails trip planner UI. See `PORTING.md` for behaviour differences.

## Develop

```bash
go test ./...                         # unit tests
# integration tests need an empty PostGIS database (the server creates the schema), e.g. start Beenthere once against it:
BT_TEST_DATABASE_URL=postgres://... BT_TEST_ADMIN_URL=postgres://... go test -tags integration ./...
```

Licensed AGPL-3.0, like Dawarich.
