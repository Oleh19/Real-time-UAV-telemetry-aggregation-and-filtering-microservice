# UAV Telemetry Monitor

![Go](https://img.shields.io/badge/Go-1.22-00ADD8?logo=go&logoColor=white)
![gRPC](https://img.shields.io/badge/gRPC-Protobuf-5b8db8)
![NATS](https://img.shields.io/badge/NATS-JetStream-27AAE1?logo=natsdotio&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-PostGIS-4169E1?logo=postgresql&logoColor=white)
![Angular](https://img.shields.io/badge/Angular-22-DD0031?logo=angular&logoColor=white)
![TypeScript](https://img.shields.io/badge/TypeScript-strict-3178C6?logo=typescript&logoColor=white)
![Leaflet](https://img.shields.io/badge/Leaflet-OpenStreetMap-199900?logo=leaflet&logoColor=white)
![nginx](https://img.shields.io/badge/nginx-reverse_proxy-009639?logo=nginx&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white)

Real-time hostile UAV monitoring system: a detection network streams target tracks into a gRPC aggregation server, a geofence worker raises per-oblast air alerts from PostGIS boundary checks through NATS JetStream, and a live web dashboard shows the air picture. A simulator plays the role of the detection network tracking drones that fly in from outside Ukraine.

## Features

- **Streaming ingest** — the detection network pushes target track reports over gRPC client streams; a worker pool with a bounded queue applies backpressure and consciously drops overflow instead of falling over.
- **Authenticated & hardened** — bearer-token auth on the ingest path, gRPC message-size limits, distroless read-only containers with dropped capabilities, and nginx security headers + rate limiting (see [Security](#security)).
- **Validation and filtering** — coordinate ranges, track confidence bounds, and timestamp drift are checked on every sample; invalid samples are rejected and counted without killing the stream.
- **Durable pipeline** — samples flow through a NATS JetStream stream with durable consumers: if the geofence worker is down, telemetry waits in the stream and is processed after restart, with zero loss.
- **Oblast air-alert system** — real boundaries of all 27 Ukrainian regions (geoBoundaries data) with a 10 km alert buffer around each one, seeded into PostGIS on first start; `ST_Intersects` checks on every position raise an alarm for the oblast once per entry (no per-sample spam), publish a `ZoneBreach` event to `drone.alerts`, and track exits; an oblast stays alarmed while at least one drone is inside its buffer.
- **Flight history** — batched idempotent inserts into PostGIS with automatic retention pruning.
- **Last known state** — per-drone in-memory cache with TTL eviction and out-of-order protection.
- **Live dashboard** — Leaflet map of Ukraine with heading-oriented target triangles colored by track confidence and oblast boundaries that flash red while alarmed, an air-alert panel listing every oblast, ingest metric tiles with connection status, and a tracked-targets table, refreshed every second.
- **Observability** — health checks on every service, ingest counters, graceful shutdown with drain.

## Quick start

The only prerequisite is Docker — no Go, Node, or database installed on the host. Build and start everything:

```bash
docker compose up --build
```

No configuration is required: the database schema is created automatically by versioned migrations on first start, oblast alert zones are seeded from embedded data, and every service reports a Docker healthcheck so startup is ordered correctly. To override defaults (e.g. the database password), copy `.env.example` to `.env` and edit it.

Then verify the system end to end:

1. Open `http://localhost:4200` — the map shows Ukraine with drone triangles (oriented along their flight direction) flying in from outside the borders; when a drone enters the 10 km alert buffer of an oblast, the oblast polygon on the map and its chip in the Air alerts panel turn red until the drone leaves. Drones get shot down and new ones keep arriving, so the number of tracked targets fluctuates. The Ingest panel shows `online` with growing counters, and the Tracked targets table lists every active track.
2. Watch alerts: `docker compose logs -f geofence` — `ALERT: Drone <id> entered the alert zone of <oblast>!` on entry, `drone left alert zone` on exit.
3. Check durability: `docker compose stop geofence`, wait ~30 seconds, `docker compose start geofence` — the worker drains the JetStream backlog and no telemetry is lost (history keeps growing without gaps).
4. Run the test suites: `make check` for the backend, `make web-test` for the frontend.
5. Load test: `DRONE_COUNT=150 SEND_INTERVAL=100ms docker compose up -d simulator` pushes ~1500 msg/s through the pipeline; watch `/metrics` and consumer lag, then restore with `docker compose up -d simulator` (defaults are 10 drones at 500ms).

Stop everything with `docker compose down` (add `-v` to also wipe database and stream data).

## Architecture

```
simulator (N drones) --gRPC client-stream--> server --JetStream drone.telemetry--> geofence worker --SQL--> PostgreSQL + PostGIS
                                               |                                        |
                                 validation + last-state cache          batched telemetry_history + oblast air alerts
```

- `cmd/server` — gRPC server, accepts `StreamTelemetry` client streams, validates every sample (coordinate ranges, track confidence, timestamp drift; invalid samples are rejected and counted, not fatal), publishes accepted samples to the JetStream `DRONE` stream through a worker pool, keeps last known state per drone in `sync.Map` with TTL eviction and out-of-order protection, exposes HTTP observability endpoints.
- `cmd/simulator` — plays the detection network: runs up to N concurrent target slots; each hostile drone spawns outside the borders of Ukraine, flies in towards random waypoints across the country while its track (position, altitude, speed, confidence) is streamed every 500ms over gRPC, and is eventually shot down (average lifetime ~3 minutes); after a random pause the slot picks up a new target with a fresh track id, so the number of active tracks constantly fluctuates.
- `cmd/geofence` — seeds oblast boundaries with 10 km alert buffers on first start, then runs two durable JetStream consumers over `drone.telemetry`: `geofence-history` batches samples into `telemetry_history` (pipelined inserts, idempotent via `ON CONFLICT DO NOTHING`, nak + redelivery on failure), and `geofence-zones` runs PostGIS `ST_Intersects` checks against oblast alert zones with a worker pool; an alert fires once per zone entry (not per sample), is logged and published as a `ZoneBreach` event to `drone.alerts`; exits are logged. History older than the retention window is pruned. Exposes `GET /zones` (oblast boundaries as GeoJSON) and `GET /alerts` (per-oblast alarm status).
- `frontend/` — Angular dashboard (standalone components, signals, OnPush) served by nginx at `http://localhost:4200`: live Leaflet map with heading-oriented target triangles colored by track confidence, oblast polygons that turn red while alarmed, an air-alert panel, ingest metric tiles with connection status, and a tracked-targets table. It consumes two Server-Sent Events streams (`/api/stream` for drones + metrics, `/api/alert-stream` for oblast alerts) instead of polling, so the dashboard is push-updated. nginx proxies `/api/*` to the backend services.

### Vocabulary

The code and wire protocol use **drone** as the domain term (the physical object being tracked: `DroneTelemetry`, `drone.telemetry`, `DroneSample`, `telemetry_history`). The operator-facing UI presents these as **targets** / **tracks** — the detection network's view of each drone, with a `confidence` score for how firmly it is being followed. This split is deliberate: `target` never appears as a code identifier, only as display copy.

## Observability

- `http://localhost:8080/healthz` — server liveness
- `http://localhost:8080/metrics` — ingest counters (received / published / dropped / failed / rejected)
- `http://localhost:8080/drones` — last known state of every drone
- `http://localhost:8080/events` — SSE stream of drones + ingest metrics
- `http://localhost:8081/healthz` — geofence worker liveness (includes DB ping)
- `http://localhost:8081/zones` — oblast boundaries as GeoJSON
- `http://localhost:8081/alerts` — per-oblast air-alert status (alarmed + drone count)
- `http://localhost:8081/events` — SSE stream of per-oblast alert status
- `http://localhost:4200` — web dashboard (map, alerts, metrics, targets)

## Configuration

All services are configured via environment variables (see `docker-compose.yml`):

| Service   | Variable            | Default                                              |
| --------- | ------------------- | ---------------------------------------------------- |
| server    | `GRPC_ADDR`         | `:50051`                                             |
| server    | `HTTP_ADDR`         | `:8080`                                              |
| server    | `NATS_URL`          | `nats://localhost:4222`                              |
| server    | `WORKER_COUNT`      | `8`                                                  |
| server    | `QUEUE_SIZE`        | `1024`                                               |
| server    | `STATE_TTL`         | `5m` (`30s` in compose)                              |
| server    | `INGEST_TOKEN`      | _(empty = auth off)_ (`dev-ingest-token` in compose) |
| simulator | `SERVER_ADDR`       | `localhost:50051`                                    |
| simulator | `INGEST_TOKEN`      | _(empty)_ (`dev-ingest-token` in compose)            |
| simulator | `DRONE_COUNT`       | `5` (`10` in compose)                                |
| simulator | `SEND_INTERVAL`     | `500ms`                                              |
| geofence  | `POSTGRES_DSN`      | `postgres://uav:uav@localhost:5432/uav`              |
| postgres  | `POSTGRES_PASSWORD` | `uav` (override via `.env`)                          |
| geofence  | `HTTP_ADDR`         | `:8081`                                              |
| geofence  | `WORKER_COUNT`      | `8`                                                  |
| geofence  | `QUEUE_SIZE`        | `256`                                                |
| geofence  | `HISTORY_RETENTION` | `24h`                                                |
| geofence  | `BATCH_SIZE`        | `100`                                                |
| geofence  | `FLUSH_INTERVAL`    | `1s`                                                 |

## Development (everything runs in Docker)

All toolchain commands are wrapped in the Makefile and execute inside containers:

```bash
make test            # go test -race (unit)
make itest           # integration tests vs ephemeral PostGIS + NATS
make vet             # go vet ./...
make lint            # golangci-lint run
make fmt             # gofmt -w cmd internal
make prettier        # prettier --write (yaml, json, md, future frontend)
make prettier-check  # prettier --check
make tidy            # go mod tidy
make proto           # buf generate
make check           # vet + lint + test + prettier-check
make web-install     # npm ci for the frontend
make web-lint        # ng lint
make web-test        # ng test (headless chromium)
make web-build       # ng build
make up              # docker compose up --build
```

## Layout

```
api/v1/               protobuf contract
gen/telemetryv1/      generated protobuf + gRPC code
cmd/server/           aggregation server entrypoint
cmd/simulator/        detection network simulator entrypoint
cmd/geofence/         geofence worker entrypoint
internal/config/      env-based configuration
internal/telemetry/   domain types
internal/usecase/     ingest worker pool + last-state cache with TTL eviction
internal/delivery/    gRPC handlers
internal/queue/       NATS connection helper, JetStream stream setup, telemetry/alert publishers
internal/geofence/    JetStream consumers: batched history writer, zone checker with enter/exit state, retention
internal/repository/  PostGIS repository
internal/repository/postgres/migrations/  versioned SQL schema migrations
frontend/             Angular dashboard (Leaflet map, alerts, targets table) + nginx
.github/workflows/    CI (backend check + integration, frontend, docker build)
```

## Security

- **Authenticated ingest** — `StreamTelemetry` is guarded by a bearer-token gRPC interceptor (constant-time comparison). The server rejects unauthenticated streams with `Unauthenticated` when `INGEST_TOKEN` is set; compose wires a shared dev token to server and simulator so local runs work out of the box. Override it via `.env` and never ship the default.
- **Message-size limit** — gRPC `MaxRecvMsgSize` caps ingest messages (64 KiB) as a DoS guard, alongside `MaxConcurrentStreams` and keepalive enforcement.
- **Hardened containers** — the Go services run distroless as non-root with `read_only` root filesystems, `cap_drop: ALL`, and `no-new-privileges`; every service sets `no-new-privileges`.
- **nginx** — security headers on every response (`Content-Security-Policy`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy`), version banner disabled, and per-IP rate limiting on the JSON API.
- **Secrets** — the database password and ingest token come from the environment (`.env`), with local-friendly defaults; production supplies real values.

## Production readiness

This is a portfolio-grade project that runs and self-verifies locally with one command. What is in place: authenticated ingest, hardened containers, durable delivery, schema migrations, healthchecks, integration tests, CI, and graceful shutdown. Deliberately **out of scope** for a local-first demo (and easy to add when a real deployment target exists):

- **Transport encryption** — traffic between containers on the private Docker network is plaintext; a real deployment would terminate TLS (and ideally mTLS) on the gRPC ingest path. The auth interceptor and per-RPC credentials are already in place, so enabling TLS is a transport-credentials swap, not an app change.
- **Horizontal scale** — the last-state cache and per-oblast alarm state live in process memory, so the aggregation server and geofence worker run as single instances; scaling out would require sharding telemetry by drone id or moving that state to a shared store (e.g. Redis).
- **Metrics stack** — metrics are exposed as JSON/SSE; a production setup would add a Prometheus exposition endpoint, scraper, dashboards, and alerting on `dropped`/`failed`.
- **History at scale** — `telemetry_history` retention is a periodic `DELETE`; at hundreds of millions of rows this should become time-based partitioning (drop-partition retention).

## Credits

Oblast boundaries are derived from [geoBoundaries](https://www.geoboundaries.org/) (Runfola et al., 2020), licensed under **CC BY 4.0**.
