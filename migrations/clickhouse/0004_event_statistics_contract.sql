ALTER TABLE tracking_event ADD COLUMN IF NOT EXISTS count_pv Bool DEFAULT true AFTER time_corrected;
ALTER TABLE tracking_event ADD COLUMN IF NOT EXISTS count_uv Bool DEFAULT true AFTER count_pv;
ALTER TABLE tracking_event ADD COLUMN IF NOT EXISTS uv_mode LowCardinality(String) DEFAULT 'APPROXIMATE' AFTER count_uv;
