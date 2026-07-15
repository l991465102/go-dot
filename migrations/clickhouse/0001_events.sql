CREATE TABLE IF NOT EXISTS tracking_event (
    event_id String,
    batch_id String,
    project_id UInt64,
    point_id UInt64,
    point_code String,
    point_version UInt32,
    domain_category_id UInt64,
    domain_category_code String,
    module_category_id UInt64,
    module_category_code String,
    scene_category_id UInt64,
    scene_category_code String,
    visitor_hash FixedString(64),
    platform LowCardinality(String),
    channel LowCardinality(String),
    app_version LowCardinality(String),
    session_hash String,
    client_event_at DateTime64(3, 'UTC'),
    received_at DateTime64(3, 'UTC'),
    statistic_at DateTime64(3, 'UTC'),
    statistic_date Date,
    time_corrected Bool,
    properties_json String,
    version UInt64
)
ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(received_at)
ORDER BY (project_id, point_id, statistic_date, statistic_at, event_id)
TTL received_at + INTERVAL 30 DAY DELETE;

CREATE TABLE IF NOT EXISTS tracking_invalid_event (
    project_id UInt64,
    batch_id String,
    event_id String,
    point_code String,
    platform LowCardinality(String),
    channel LowCardinality(String),
    error_code LowCardinality(String),
    error_message String,
    raw_json String,
    received_at DateTime64(3, 'UTC'),
    version UInt64
)
ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(received_at)
ORDER BY (project_id, received_at, event_id, error_code)
TTL received_at + INTERVAL 30 DAY DELETE;
