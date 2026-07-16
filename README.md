# UAV Telemetry Monitor

Real-time tactical UAV monitoring skeleton: a gRPC telemetry aggregation server, a drone fleet simulator, and a geofence worker that checks positions against PostGIS no-fly zones via NATS.

## Architecture

```
simulator (N drones) --gRPC client-stream--> server --JetStream drone.telemetry--> geofence worker --SQL--> PostgreSQL + PostGIS
                                               |                                        |
                                 validation + last-state cache          batched telemetry_history + no-fly zone alerts
```

- `cmd/server` — gRPC server, accepts `StreamTelemetry` client streams, validates every sample (coordinate ranges, battery, timestamp drift; invalid samples are rejected and counted, not fatal), publishes accepted samples to the JetStream `DRONE` stream through a worker pool, keeps last known state per drone in `sync.Map` with TTL eviction and out-of-order protection, exposes HTTP observability endpoints.
- `cmd/simulator` — spawns N goroutines, one per drone; each sends randomized telemetry every 500ms over a gRPC stream.
- `cmd/geofence` — two durable JetStream consumers over `drone.telemetry`: `geofence-history` batches samples into `telemetry_history` (pipelined inserts, idempotent via `ON CONFLICT DO NOTHING`, nak + redelivery on failure), and `geofence-zones` runs PostGIS `ST_Intersects` checks with a worker pool; a breach alert fires once per zone entry (not per sample), is logged as `ALERT: Drone <id> breached No-Fly Zone <name>!`, and is published as a `ZoneBreach` event to `drone.alerts`; zone exits are logged. History older than the retention window is pruned.

## Run

```bash
docker compose up --build
```

Watch geofence alerts:

```bash
docker compose logs -f geofence
```

## Observability

- `http://localhost:8080/healthz` — server liveness
- `http://localhost:8080/metrics` — ingest counters (received / published / dropped / failed / rejected)
- `http://localhost:8080/drones` — last known state of every drone
- `http://localhost:8081/healthz` — geofence worker liveness (includes DB ping)

## Configuration

All services are configured via environment variables (see `docker-compose.yml`):

| Service   | Variable            | Default                  |
|-----------|---------------------|--------------------------|
| server    | `GRPC_ADDR`         | `:50051`                 |
| server    | `HTTP_ADDR`         | `:8080`                  |
| server    | `NATS_URL`          | `nats://localhost:4222`  |
| server    | `WORKER_COUNT`      | `8`                      |
| server    | `QUEUE_SIZE`        | `1024`                   |
| server    | `STATE_TTL`         | `5m`                     |
| simulator | `SERVER_ADDR`       | `localhost:50051`        |
| simulator | `DRONE_COUNT`       | `5`                      |
| simulator | `SEND_INTERVAL`     | `500ms`                  |
| geofence  | `POSTGRES_DSN`      | `postgres://uav:uav@localhost:5432/uav` |
| geofence  | `HTTP_ADDR`         | `:8081`                  |
| geofence  | `WORKER_COUNT`      | `8`                      |
| geofence  | `QUEUE_SIZE`        | `256`                    |
| geofence  | `HISTORY_RETENTION` | `24h`                    |
| geofence  | `BATCH_SIZE`        | `100`                    |
| geofence  | `FLUSH_INTERVAL`    | `1s`                     |

## Development (everything runs in Docker)

All toolchain commands are wrapped in the Makefile and execute inside containers:

```bash
make test    # go test -race ./...
make vet     # go vet ./...
make lint    # golangci-lint run
make fmt     # gofmt -w cmd internal
make tidy    # go mod tidy
make proto   # buf generate
make check   # vet + lint + test
make up      # docker compose up --build
```

## Layout

```
api/v1/               protobuf contract
gen/telemetryv1/      generated protobuf + gRPC code
cmd/server/           aggregation server entrypoint
cmd/simulator/        drone fleet simulator entrypoint
cmd/geofence/         geofence worker entrypoint
internal/config/      env-based configuration
internal/telemetry/   domain types
internal/usecase/     ingest worker pool + last-state cache with TTL eviction
internal/delivery/    gRPC handlers
internal/queue/       NATS connection helper, JetStream stream setup, telemetry/alert publishers
internal/geofence/    JetStream consumers: batched history writer, zone checker with enter/exit state, retention
internal/repository/  PostGIS repository
deploy/init.sql       schema + seed no-fly zone
```
