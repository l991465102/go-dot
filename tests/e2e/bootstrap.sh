#!/usr/bin/env bash
set -euo pipefail

project=${GO_DOT_E2E_COMPOSE_PROJECT:-go-dot-acceptance}
override=${GO_DOT_E2E_COMPOSE_OVERRIDE:-deploy/docker-compose.acceptance.yml}
env_file=${GO_DOT_E2E_ENV_FILE:-.env.acceptance}
workflow_admin=${GO_DOT_E2E_WORKFLOW_ADMIN_USERNAME:-e2e_admin}

[[ "$workflow_admin" =~ ^[a-z][a-z0-9_]{2,63}$ ]] || { echo "invalid GO_DOT_E2E_WORKFLOW_ADMIN_USERNAME" >&2; exit 1; }

if [[ ! -f "$env_file" ]]; then
  echo "missing $env_file; copy deploy/acceptance.env.example first" >&2
  exit 1
fi

compose=(docker compose --env-file "$env_file" -p "$project" -f docker-compose.yml -f "$override")
"${compose[@]}" exec -T mysql sh -c 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD" go_dot' <<SQL
INSERT INTO user_account(username,password_hash,display_name,must_change_password,status)
SELECT '${workflow_admin}',password_hash,'E2E Administrator',FALSE,'ACTIVE'
FROM user_account WHERE username='admin'
ON DUPLICATE KEY UPDATE password_hash=VALUES(password_hash),display_name=VALUES(display_name),must_change_password=FALSE,status='ACTIVE';

INSERT INTO user_account(username,password_hash,display_name,must_change_password,status)
SELECT 'e2e_first',password_hash,'E2E First Login',TRUE,'ACTIVE'
FROM user_account WHERE username='admin'
ON DUPLICATE KEY UPDATE password_hash=VALUES(password_hash),display_name=VALUES(display_name),must_change_password=TRUE,status='ACTIVE';

INSERT INTO user_account(username,password_hash,display_name,must_change_password,status)
SELECT 'e2e_viewer',password_hash,'E2E Viewer',FALSE,'ACTIVE'
FROM user_account WHERE username='admin'
ON DUPLICATE KEY UPDATE password_hash=VALUES(password_hash),display_name=VALUES(display_name),must_change_password=FALSE,status='ACTIVE';
SQL

echo "E2E fixtures ready: ${workflow_admin}, e2e_first and e2e_viewer use the current admin password"
