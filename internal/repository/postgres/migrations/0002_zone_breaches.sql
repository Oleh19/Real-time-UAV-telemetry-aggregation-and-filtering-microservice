CREATE TABLE IF NOT EXISTS zone_breaches (
    id BIGSERIAL PRIMARY KEY,
    drone_id TEXT NOT NULL,
    zone_id BIGINT NOT NULL,
    zone_name TEXT NOT NULL,
    event TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    position GEOMETRY(POINT, 4326) NOT NULL,
    altitude DOUBLE PRECISION NOT NULL,
    inserted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS zone_breaches_dedup_key ON zone_breaches (drone_id, zone_id, event, occurred_at);

CREATE INDEX IF NOT EXISTS zone_breaches_occurred_at_idx ON zone_breaches (occurred_at DESC);
