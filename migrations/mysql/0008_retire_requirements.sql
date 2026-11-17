-- 保留历史数据，解除已停用任务对点位删除的限制；重复执行时安全跳过。
SET @go_dot_task_fk_exists = (SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS WHERE CONSTRAINT_SCHEMA = DATABASE() AND TABLE_NAME = 'tracking_requirement_task' AND CONSTRAINT_NAME = 'fk_task_point' AND CONSTRAINT_TYPE = 'FOREIGN KEY');
SET @go_dot_task_fk_sql = IF(@go_dot_task_fk_exists > 0, 'ALTER TABLE tracking_requirement_task DROP FOREIGN KEY fk_task_point', 'DO 0');
PREPARE go_dot_task_fk_stmt FROM @go_dot_task_fk_sql;
EXECUTE go_dot_task_fk_stmt;
DEALLOCATE PREPARE go_dot_task_fk_stmt;
