CREATE TABLE IF NOT EXISTS tracking_property_event (
    event_id String,
    project_id UInt64,
    point_id UInt64,
    point_code String,
    point_version UInt32,
    property_code String,
    property_type LowCardinality(String),
    value_string String,
    value_integer Nullable(Int64),
    value_decimal Nullable(Decimal(38, 9)),
    visitor_hash FixedString(64),
    uv_mode LowCardinality(String),
    statistic_at DateTime64(3, 'UTC'),
    statistic_date Date,
    received_at DateTime64(3, 'UTC'),
    version UInt64
)
ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(received_at)
ORDER BY (project_id, point_id, property_code, statistic_date, statistic_at, event_id)
TTL received_at + INTERVAL 30 DAY DELETE;

CREATE TABLE IF NOT EXISTS property_metric_hourly (
    hour DateTime('UTC'),
    project_id UInt64,
    point_id UInt64,
    point_code String,
    point_version UInt32,
    property_code String,
    value_count UInt64,
    value_sum Decimal(38, 9),
    value_min Nullable(Decimal(38, 9)),
    value_max Nullable(Decimal(38, 9)),
    version UInt64
)
ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(hour)
ORDER BY (project_id, hour, point_id, point_version, property_code)
TTL hour + INTERVAL 400 DAY DELETE;

CREATE TABLE IF NOT EXISTS property_dimension_hourly_approximate (
    hour DateTime('UTC'),
    project_id UInt64,
    point_id UInt64,
    point_code String,
    point_version UInt32,
    property_code String,
    property_value String,
    event_count UInt64,
    uv_state AggregateFunction(uniqCombined64, FixedString(64)),
    version UInt64
)
ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(hour)
ORDER BY (project_id, hour, point_id, point_version, property_code, property_value)
TTL hour + INTERVAL 400 DAY DELETE;

CREATE TABLE IF NOT EXISTS property_dimension_hourly_exact (
    hour DateTime('UTC'),
    project_id UInt64,
    point_id UInt64,
    point_code String,
    point_version UInt32,
    property_code String,
    property_value String,
    event_count UInt64,
    uv_state AggregateFunction(uniqExact, FixedString(64)),
    version UInt64
)
ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(hour)
ORDER BY (project_id, hour, point_id, point_version, property_code, property_value)
TTL hour + INTERVAL 400 DAY DELETE;

CREATE TABLE IF NOT EXISTS property_stat_hour_generation (
    project_id UInt64,
    hour DateTime('UTC'),
    generation UInt64,
    committed_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(generation)
PARTITION BY toYYYYMM(hour)
ORDER BY (project_id, hour)
TTL hour + INTERVAL 400 DAY DELETE;
