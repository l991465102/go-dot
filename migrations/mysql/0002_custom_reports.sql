CREATE TABLE IF NOT EXISTS custom_report (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    project_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(128) NOT NULL,
    description VARCHAR(1000) NOT NULL DEFAULT '',
    default_days SMALLINT UNSIGNED NOT NULL DEFAULT 30,
    created_by BIGINT UNSIGNED NOT NULL,
    updated_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_custom_report_project_name (project_id, name),
    KEY idx_custom_report_project (project_id, id),
    CONSTRAINT fk_custom_report_project FOREIGN KEY (project_id) REFERENCES project (id),
    CONSTRAINT fk_custom_report_created_by FOREIGN KEY (created_by) REFERENCES user_account (id),
    CONSTRAINT fk_custom_report_updated_by FOREIGN KEY (updated_by) REFERENCES user_account (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS custom_report_point (
    report_id BIGINT UNSIGNED NOT NULL,
    point_id BIGINT UNSIGNED NOT NULL,
    sort_no SMALLINT UNSIGNED NOT NULL,
    PRIMARY KEY (report_id, point_id),
    UNIQUE KEY uk_custom_report_point_order (report_id, sort_no),
    KEY idx_custom_report_point (point_id),
    CONSTRAINT fk_custom_report_point_report FOREIGN KEY (report_id) REFERENCES custom_report (id) ON DELETE CASCADE,
    CONSTRAINT fk_custom_report_point_point FOREIGN KEY (point_id) REFERENCES tracking_point (id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
