-- Initial schema for WoW Tools Forever AH community sync server.

CREATE TABLE IF NOT EXISTS markets (
    id            SERIAL PRIMARY KEY,
    region        TEXT NOT NULL,
    realm         TEXT NOT NULL,
    faction       TEXT NOT NULL,
    display_realm TEXT NOT NULL,
    latest_day    INT NOT NULL DEFAULT 0,
    snapshot_ver  INT NOT NULL DEFAULT 0,
    snapshot_day  INT NOT NULL DEFAULT 0,
    snapshot_at   TIMESTAMPTZ NOT NULL DEFAULT 'epoch',
    snapshot_sha  TEXT NOT NULL DEFAULT '',
    snapshot_size INT NOT NULL DEFAULT 0,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (region, realm, faction)
);

CREATE TABLE IF NOT EXISTS installs (
    id            UUID PRIMARY KEY,
    first_seen    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen     TIMESTAMPTZ NOT NULL DEFAULT now(),
    app_version   TEXT NOT NULL DEFAULT '',
    upload_count  INT NOT NULL DEFAULT 0,
    anomaly_runs  SMALLINT NOT NULL DEFAULT 0,
    quarantined   BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS uploads (
    id             BIGSERIAL PRIMARY KEY,
    market_id      INT NOT NULL REFERENCES markets(id),
    install_id     UUID NOT NULL REFERENCES installs(id),
    ip_hash        BYTEA NOT NULL,
    client_scan_ts TIMESTAMPTZ,
    item_day_count INT NOT NULL,
    is_full_scan   BOOLEAN NOT NULL DEFAULT FALSE,
    payload_sha256 TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_uploads_rate ON uploads (install_id, market_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_uploads_ip_rate ON uploads (ip_hash, created_at DESC);

-- Rolling raw per-install observations (kept 30 days for consensus re-aggregation).
CREATE TABLE IF NOT EXISTS observations (
    market_id    INT NOT NULL REFERENCES markets(id),
    day          INT NOT NULL,
    item_id      INT NOT NULL,
    install_id   UUID NOT NULL REFERENCES installs(id),
    low          BIGINT NOT NULL,
    high         BIGINT NOT NULL,
    offers       INT NOT NULL,
    last_price   BIGINT,
    is_full_scan BOOLEAN NOT NULL DEFAULT FALSE,
    observed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (market_id, day, item_id, install_id)
);
CREATE INDEX IF NOT EXISTS idx_observations_observed_at ON observations (observed_at);

-- Permanent consolidated daily price history (unlimited retention).
CREATE TABLE IF NOT EXISTS daily_prices (
    market_id      INT NOT NULL REFERENCES markets(id),
    day            INT NOT NULL,
    item_id        INT NOT NULL,
    low            BIGINT NOT NULL,
    high           BIGINT NOT NULL,
    offers         INT NOT NULL,
    reporters      SMALLINT NOT NULL,
    full_reporters SMALLINT NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (market_id, day, item_id)
);
CREATE INDEX IF NOT EXISTS idx_daily_prices_item ON daily_prices (market_id, item_id, day DESC);

-- Latest known buyout ('m' equivalent) per item.
CREATE TABLE IF NOT EXISTS latest_prices (
    market_id   INT NOT NULL REFERENCES markets(id),
    item_id     INT NOT NULL,
    price       BIGINT NOT NULL,
    day         INT NOT NULL,
    reporters   SMALLINT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (market_id, item_id)
);

-- Tracks dirty days and immutable revision numbers for CDN caching.
CREATE TABLE IF NOT EXISTS day_revisions (
    market_id  INT NOT NULL REFERENCES markets(id),
    day        INT NOT NULL,
    rev        INT NOT NULL DEFAULT 0,
    dirty      BOOLEAN NOT NULL DEFAULT TRUE,
    item_count INT NOT NULL DEFAULT 0,
    sha256     TEXT NOT NULL DEFAULT '',
    byte_size  INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (market_id, day)
);
