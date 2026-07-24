CREATE TABLE IF NOT EXISTS friendly_squawks (
    code TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO friendly_squawks (code, label) VALUES
    ('UAF-01', 'Air Force patrol'),
    ('UAF-02', 'Border guard UAV'),
    ('MED-01', 'Medical evacuation')
ON CONFLICT (code) DO NOTHING;
