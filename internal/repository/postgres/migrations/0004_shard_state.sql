CREATE TABLE IF NOT EXISTS zone_presence (
    replica TEXT NOT NULL,
    zone_id BIGINT NOT NULL,
    drones INTEGER NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (replica, zone_id)
);

CREATE INDEX IF NOT EXISTS zone_presence_updated_at_idx ON zone_presence (updated_at);

CREATE TABLE IF NOT EXISTS active_swarms (
    replica TEXT NOT NULL,
    swarm_id TEXT NOT NULL,
    drone_ids JSONB NOT NULL,
    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,
    detected_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (replica, swarm_id)
);

CREATE INDEX IF NOT EXISTS active_swarms_updated_at_idx ON active_swarms (updated_at);
