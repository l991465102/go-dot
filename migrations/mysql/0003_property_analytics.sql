ALTER TABLE tracking_point_property
    ADD COLUMN analytics_mode VARCHAR(16) NOT NULL DEFAULT 'NONE' AFTER description,
    ADD COLUMN analytics_aggregations JSON NULL AFTER analytics_mode;

CREATE TABLE IF NOT EXISTS custom_report_property (
    report_id BIGINT UNSIGNED NOT NULL,
    point_id BIGINT UNSIGNED NOT NULL,
    property_code VARCHAR(128) NOT NULL,
    aggregations JSON NOT NULL,
    sort_no INT NOT NULL DEFAULT 0,
    PRIMARY KEY (report_id, point_id, property_code),
    UNIQUE KEY uk_custom_report_property_order (report_id, sort_no),
    KEY idx_custom_report_property_point (point_id),
    CONSTRAINT fk_custom_report_property_report FOREIGN KEY (report_id) REFERENCES custom_report (id) ON DELETE CASCADE,
    CONSTRAINT fk_custom_report_property_point FOREIGN KEY (point_id) REFERENCES tracking_point (id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
