CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE IF NOT EXISTS nofly_zones (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    boundary GEOMETRY(POLYGON, 4326) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS nofly_zones_boundary_gist ON nofly_zones USING GIST (boundary);

CREATE TABLE IF NOT EXISTS telemetry_history (
    id BIGSERIAL PRIMARY KEY,
    drone_id TEXT NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL,
    position GEOMETRY(POINT, 4326) NOT NULL,
    altitude DOUBLE PRECISION NOT NULL,
    speed REAL NOT NULL,
    battery_percentage INTEGER NOT NULL,
    inserted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS telemetry_history_drone_recorded_key ON telemetry_history (drone_id, recorded_at);
CREATE INDEX IF NOT EXISTS telemetry_history_position_gist ON telemetry_history USING GIST (position);

INSERT INTO nofly_zones (name, boundary)
VALUES (
    'Restricted Object Alpha',
    ST_SetSRID(ST_GeomFromText('POLYGON((30.50 50.44, 30.56 50.44, 30.56 50.47, 30.50 50.47, 30.50 50.44))'), 4326)
)
ON CONFLICT (name) DO NOTHING;
