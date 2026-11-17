CREATE TABLE IF NOT EXISTS tracking_test_event (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    project_id BIGINT UNSIGNED NOT NULL,
    point_code VARCHAR(128) NOT NULL,
    platform VARCHAR(32) NOT NULL,
    app_version VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL,
    error_code VARCHAR(128) NOT NULL,
    received_at DATETIME(6) NOT NULL,
    KEY idx_test_event_project_point_time (project_id, point_code, received_at, id),
    KEY idx_test_event_expiry (received_at),
    CONSTRAINT fk_test_event_project FOREIGN KEY (project_id) REFERENCES project(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
