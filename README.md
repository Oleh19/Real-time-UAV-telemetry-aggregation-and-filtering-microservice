# UAV Telemetry Monitor

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
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
- **Multi-sensor track fusion** — several detection stations report the same target with noise and their own local track IDs; each canonical track runs a constant-velocity Kalman filter in a local metric plane, cross-station observations are associated by Mahalanobis gating against the predicted state (tracks from one station never merge with each other), the filter smooths positions, estimates velocity, and predicts through missed frames, and the whole downstream pipeline sees one `target-NNN` track.
- **Subscribe API for integrations** — external consumers call the server-streaming `SubscribeTelemetry` RPC (optionally filtered by drone IDs and minimum track confidence, guarded by the same bearer token) and receive the live fused track stream; a fan-out hub gives every subscriber its own buffer and drops samples for slow consumers instead of stalling ingest.
- **Swarm detection** — the geofence worker clusters live target positions every few seconds (grid-bucketed union-find within a configurable radius) and raises a swarm alert when three or more targets move as a compact group, tracking the formation under a stable ID until it scatters; active swarms show on the dashboard and in `GET /swarms`. The simulator launches one coordinated formation (`SWARM_SIZE`, default 4: a leader plus followers holding formation offsets) so a swarm is always in the air.
- **History replay** — re-run any recorded incident through the live pipeline at a chosen speed: `POST /api/replays` with `{"from": "...", "to": "...", "speed": 20}` (optional `droneId`) reads the range from PostGIS and re-publishes it with rebased timestamps and `replay-NNN/`-prefixed IDs, so replayed targets appear on the map and pass through geofence checks like live traffic; `GET /api/replays` shows progress and `DELETE /api/replays/{id}` cancels.
- **Authenticated & hardened** — bearer-token auth on the ingest path, gRPC message-size limits, distroless read-only containers with dropped capabilities, and nginx security headers + rate limiting (see [Security](#security)).
- **Validation and filtering** — coordinate ranges, track confidence bounds, and timestamp drift are checked on every sample; invalid samples are rejected and counted without killing the stream.
- **Durable pipeline** — samples flow through a NATS JetStream stream with durable consumers: if the geofence worker is down, telemetry waits in the stream and is processed after restart, with zero loss.
- **Oblast air-alert system** — real boundaries of all 27 Ukrainian regions (geoBoundaries data) with a 10 km alert buffer around each one, seeded into PostGIS on first start; `ST_Intersects` checks on every position raise an alarm for the oblast once per entry (no per-sample spam), publish a `ZoneBreach` event to `drone.alerts`, and track exits; an oblast stays alarmed while at least one drone is inside its buffer.
- **Flight history** — batched idempotent inserts into PostGIS with automatic retention pruning.
- **Track playback** — click any drone on the map to load its recorded track over `GET /history`, see the flight path drawn on the map, and replay it with a play button and time scrubber.
- **Breach journal** — every zone entry and exit event flows through `drone.alerts` into a durable JetStream consumer that persists it to PostGIS; `GET /breaches` serves the log and the dashboard shows a live event feed.
- **Custom protected zones** — draw a polygon around any critical object right on the map, name it, and it joins the geofence checks alongside the oblast buffers (index refreshes immediately on create/delete); breaches of custom zones land in the same journal.
- **Course prediction** — the dashboard extrapolates each drone's velocity from consecutive positions, draws a dashed predicted path, and shows the estimated time until the drone crosses into the nearest oblast it is heading for, right in the drone tooltip.
- **Last known state** — per-drone in-memory cache with TTL eviction and out-of-order protection.
- **Live dashboard** — Leaflet map of Ukraine with heading-oriented target triangles colored by track confidence and oblast boundaries that flash red while alarmed, an air-alert panel listing every oblast, ingest metric tiles with connection status, and a tracked-targets table, refreshed every second.
- **Observability** — health checks on every service, Prometheus metrics on `/metrics` from both Go services, a provisioned Grafana dashboard (ingest rate, drops, queue depth, breach events, Go runtime), graceful shutdown with drain.
- **Distributed tracing** — OpenTelemetry spans follow each sample from the gRPC ingest through NATS JetStream into the geofence checks, alert publishing, and journal writes; trace context propagates in message headers and Jaeger collects everything over OTLP.

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
5. Open `http://localhost:3000` — Grafana with the provisioned UAV Telemetry Pipeline dashboard (anonymous viewer access; admin password defaults to `admin`). Raw Prometheus is at `http://localhost:9090`, and Jaeger traces are at `http://localhost:16686` (pick the `uav-server` or `uav-geofence` service).
6. Load test: `DRONE_COUNT=150 SEND_INTERVAL=100ms docker compose up -d simulator` pushes ~1500 msg/s through the pipeline; watch the ingest rate and queue depth panels in Grafana, then restore with `docker compose up -d simulator` (defaults are 10 drones at 500ms).

Stop everything with `docker compose down` (add `-v` to also wipe database and stream data).

## Security

- **Authenticated ingest** — `StreamTelemetry` is guarded by a bearer-token gRPC interceptor (constant-time comparison). The server rejects unauthenticated streams with `Unauthenticated` when `INGEST_TOKEN` is set; compose wires a shared dev token to server and simulator so local runs work out of the box. Override it via `.env` and never ship the default.
- **Message-size limit** — gRPC `MaxRecvMsgSize` caps ingest messages (64 KiB) as a DoS guard, alongside `MaxConcurrentStreams` and keepalive enforcement.
- **Hardened containers** — the Go services run distroless as non-root with `read_only` root filesystems, `cap_drop: ALL`, and `no-new-privileges`; every service sets `no-new-privileges`.
- **nginx** — security headers on every response (`Content-Security-Policy`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy`), version banner disabled, and per-IP rate limiting on the JSON API.
- **Secrets** — the database password and ingest token come from the environment (`.env`), with local-friendly defaults; production supplies real values.
