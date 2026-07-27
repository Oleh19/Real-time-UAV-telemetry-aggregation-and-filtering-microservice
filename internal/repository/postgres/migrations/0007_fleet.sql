CREATE TABLE IF NOT EXISTS fleet_drones (
    id TEXT PRIMARY KEY,
    model TEXT NOT NULL,
    base_lat DOUBLE PRECISION NOT NULL,
    base_lon DOUBLE PRECISION NOT NULL,
    firmware TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS fleet_missions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    drone_id TEXT NOT NULL,
    waypoints JSONB NOT NULL,
    state TEXT NOT NULL,
    progress INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO fleet_drones (id, model, base_lat, base_lon, firmware) VALUES
    ('uav-alpha', 'Leleka-100', 50.45, 30.52, '2.4.1'),
    ('uav-bravo', 'Furia', 49.84, 24.03, '2.4.1'),
    ('uav-charlie', 'Shark', 46.48, 30.74, '2.3.9')
ON CONFLICT (id) DO NOTHING;
