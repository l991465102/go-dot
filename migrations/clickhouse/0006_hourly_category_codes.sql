ALTER TABLE business_stat_hourly ADD COLUMN IF NOT EXISTS domain_category_code String AFTER domain_category_id;
ALTER TABLE business_stat_hourly ADD COLUMN IF NOT EXISTS module_category_code String AFTER module_category_id;
ALTER TABLE business_stat_hourly ADD COLUMN IF NOT EXISTS scene_category_code String AFTER scene_category_id;
ALTER TABLE business_stat_exact_hourly ADD COLUMN IF NOT EXISTS domain_category_code String AFTER domain_category_id;
ALTER TABLE business_stat_exact_hourly ADD COLUMN IF NOT EXISTS module_category_code String AFTER module_category_id;
ALTER TABLE business_stat_exact_hourly ADD COLUMN IF NOT EXISTS scene_category_code String AFTER scene_category_id;
