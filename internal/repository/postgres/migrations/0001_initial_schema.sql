CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE IF NOT EXISTS oblasts (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    boundary GEOMETRY(MULTIPOLYGON, 4326) NOT NULL,
    alert_zone GEOMETRY(MULTIPOLYGON, 4326) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS oblasts_alert_zone_gist ON oblasts USING GIST (alert_zone);

CREATE TABLE IF NOT EXISTS telemetry_history (
    id BIGSERIAL PRIMARY KEY,
    drone_id TEXT NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL,
    position GEOMETRY(POINT, 4326) NOT NULL,
    altitude DOUBLE PRECISION NOT NULL,
    speed REAL NOT NULL,
    confidence INTEGER NOT NULL,
    inserted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS telemetry_history_drone_recorded_key ON telemetry_history (drone_id, recorded_at);
