CREATE TABLE IF NOT EXISTS business_stat_hourly (
    hour DateTime('UTC'),
    project_id UInt64,
    domain_category_id UInt64,
    module_category_id UInt64,
    scene_category_id UInt64,
    point_id UInt64,
    point_code String,
    platform LowCardinality(String),
    channel LowCardinality(String),
    pv UInt64,
    uv_state AggregateFunction(uniqCombined64, FixedString(64)),
    version UInt64
)
ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(hour)
ORDER BY (project_id, hour, domain_category_id, module_category_id, scene_category_id, point_id, platform, channel);

CREATE TABLE IF NOT EXISTS business_stat_exact_hourly (
    hour DateTime('UTC'),
    project_id UInt64,
    domain_category_id UInt64,
    module_category_id UInt64,
    scene_category_id UInt64,
    point_id UInt64,
    point_code String,
    platform LowCardinality(String),
    channel LowCardinality(String),
    pv UInt64,
    uv_state AggregateFunction(uniqExact, FixedString(64)),
    version UInt64
)
ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(hour)
ORDER BY (project_id, hour, domain_category_id, module_category_id, scene_category_id, point_id, platform, channel);
