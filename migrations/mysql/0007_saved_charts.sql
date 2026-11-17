CREATE TABLE IF NOT EXISTS saved_chart (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    project_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(128) NOT NULL,
    kind VARCHAR(16) NOT NULL,
    config JSON NOT NULL,
    created_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    KEY idx_saved_chart_project (project_id,id),
    CONSTRAINT fk_saved_chart_project FOREIGN KEY (project_id) REFERENCES project(id),
    CONSTRAINT fk_saved_chart_user FOREIGN KEY (created_by) REFERENCES user_account(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
