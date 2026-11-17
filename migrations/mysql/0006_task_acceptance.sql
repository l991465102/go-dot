CREATE TABLE IF NOT EXISTS tracking_task_acceptance (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    task_id BIGINT UNSIGNED NOT NULL,
    result VARCHAR(16) NOT NULL,
    note VARCHAR(2000) NOT NULL,
    actor_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    KEY idx_task_acceptance_task (task_id, id),
    CONSTRAINT fk_acceptance_task FOREIGN KEY (task_id) REFERENCES tracking_requirement_task(id),
    CONSTRAINT fk_acceptance_actor FOREIGN KEY (actor_id) REFERENCES user_account(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
