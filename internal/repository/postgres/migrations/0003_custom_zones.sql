CREATE TABLE IF NOT EXISTS custom_zones (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    boundary GEOMETRY(POLYGON, 4326) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS custom_zones_boundary_gist ON custom_zones USING GIST (boundary);
