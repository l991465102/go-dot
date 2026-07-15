CREATE TABLE IF NOT EXISTS business_stat_hour_generation (
    project_id UInt64,
    hour DateTime('UTC'),
    generation UInt64,
    committed_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(generation)
PARTITION BY toYYYYMM(hour)
ORDER BY (project_id, hour);
