CREATE TABLE IF NOT EXISTS tracking_requirement (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    project_id BIGINT UNSIGNED NOT NULL,
    title VARCHAR(255) NOT NULL,
    goal TEXT NOT NULL,
    scenario TEXT NOT NULL,
    requirement_url VARCHAR(1024) NOT NULL DEFAULT '',
    due_date DATE NULL,
    created_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    KEY idx_requirement_project (project_id, id),
    CONSTRAINT fk_requirement_project FOREIGN KEY (project_id) REFERENCES project(id),
    CONSTRAINT fk_requirement_creator FOREIGN KEY (created_by) REFERENCES user_account(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS tracking_requirement_task (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    requirement_id BIGINT UNSIGNED NOT NULL,
    point_id BIGINT UNSIGNED NOT NULL,
    platform VARCHAR(32) NOT NULL,
    assignee_id BIGINT UNSIGNED NULL,
    state VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    UNIQUE KEY uk_requirement_point_platform (requirement_id, point_id, platform),
    CONSTRAINT fk_task_requirement FOREIGN KEY (requirement_id) REFERENCES tracking_requirement(id),
    CONSTRAINT fk_task_point FOREIGN KEY (point_id) REFERENCES tracking_point(id),
    CONSTRAINT fk_task_assignee FOREIGN KEY (assignee_id) REFERENCES user_account(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
