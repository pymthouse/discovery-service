-- Discovery service schema (standalone from NaaP plugin tables)

CREATE TABLE IF NOT EXISTS leaderboard_sources (
    kind TEXT PRIMARY KEY,
    priority INT NOT NULL DEFAULT 10,
    enabled BOOLEAN NOT NULL DEFAULT true,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS leaderboard_config (
    id TEXT PRIMARY KEY DEFAULT 'singleton',
    last_refreshed_at TIMESTAMPTZ,
    last_refreshed_by TEXT,
    known_capabilities JSONB NOT NULL DEFAULT '[]'::jsonb,
    membership_strategy TEXT NOT NULL DEFAULT 'union',
    refresh_interval_ms BIGINT NOT NULL DEFAULT 60000
);

INSERT INTO leaderboard_config (id) VALUES ('singleton') ON CONFLICT DO NOTHING;

INSERT INTO leaderboard_sources (kind, priority, enabled) VALUES
    ('livepeer-subgraph', 1, true),
    ('clickhouse-query', 2, true),
    ('naap-discover', 3, true),
    ('naap-pricing', 4, false),
    ('remote-signer', 5, false)
ON CONFLICT (kind) DO NOTHING;

CREATE TABLE IF NOT EXISTS leaderboard_dataset_rows (
    id BIGSERIAL PRIMARY KEY,
    capability TEXT NOT NULL,
    orch_uri TEXT NOT NULL,
    gpu_name TEXT NOT NULL DEFAULT '',
    gpu_gb DOUBLE PRECISION NOT NULL DEFAULT 0,
    avail DOUBLE PRECISION NOT NULL DEFAULT 0,
    total_cap DOUBLE PRECISION NOT NULL DEFAULT 0,
    price_per_unit DOUBLE PRECISION NOT NULL DEFAULT 0,
    best_lat_ms DOUBLE PRECISION,
    avg_lat_ms DOUBLE PRECISION,
    swap_ratio DOUBLE PRECISION,
    avg_avail DOUBLE PRECISION,
    score DOUBLE PRECISION NOT NULL DEFAULT 0,
    refreshed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_dataset_capability ON leaderboard_dataset_rows (capability);
CREATE INDEX IF NOT EXISTS idx_dataset_cap_score ON leaderboard_dataset_rows (capability, score DESC);
CREATE INDEX IF NOT EXISTS idx_dataset_cap_price ON leaderboard_dataset_rows (capability, price_per_unit);
CREATE INDEX IF NOT EXISTS idx_dataset_cap_lat ON leaderboard_dataset_rows (capability, avg_lat_ms);
CREATE INDEX IF NOT EXISTS idx_dataset_cap_gpu ON leaderboard_dataset_rows (capability, gpu_gb);
CREATE INDEX IF NOT EXISTS idx_dataset_orch_uri ON leaderboard_dataset_rows (orch_uri);

CREATE TABLE IF NOT EXISTS leaderboard_refresh_audit (
    id BIGSERIAL PRIMARY KEY,
    refreshed_by TEXT NOT NULL,
    duration_ms BIGINT NOT NULL,
    membership_source TEXT NOT NULL,
    total_orchestrators INT NOT NULL,
    total_capabilities INT NOT NULL,
    per_source JSONB NOT NULL DEFAULT '{}'::jsonb,
    conflicts JSONB NOT NULL DEFAULT '[]'::jsonb,
    dropped JSONB NOT NULL DEFAULT '[]'::jsonb,
    warnings JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
