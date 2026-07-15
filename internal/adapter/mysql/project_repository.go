package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/project"
)

var _ ports.ProjectRepository = (*ProjectRepository)(nil)

type ProjectRepository struct{ db *sql.DB }

func NewProjectRepository(db *sql.DB) *ProjectRepository { return &ProjectRepository{db: db} }

func (r *ProjectRepository) CreateProject(ctx context.Context, p project.Project, m project.Member) (created project.Project, err error) {
	origins, err := json.Marshal(nonNilStrings(p.AllowedOrigins))
	if err != nil {
		return created, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return created, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	result, err := tx.ExecContext(ctx, `INSERT INTO project(code,name,write_key_hash,allowed_origins,timezone,visitor_salt,is_active,created_by) VALUES(?,?,?,?,?,?,?,?)`, p.Code, p.Name, p.WriteKeyHash, origins, p.Timezone, p.VisitorSalt, p.Active, p.CreatedBy)
	if err != nil {
		return created, fmt.Errorf("insert project: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return created, err
	}
	p.ID = uint64(id)
	_, err = tx.ExecContext(ctx, `INSERT INTO project_member(project_id,user_id,role,is_active) VALUES(?,?,?,?)`, p.ID, m.UserID, m.Role, true)
	if err != nil {
		return created, fmt.Errorf("insert project admin: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return created, err
	}
	return p, nil
}
func (r *ProjectRepository) FindMember(ctx context.Context, userID, projectID uint64) (project.Member, error) {
	var m project.Member
	err := r.db.QueryRowContext(ctx, `SELECT user_id,project_id,role,is_active FROM project_member WHERE user_id=? AND project_id=? LIMIT 1`, userID, projectID).Scan(&m.UserID, &m.ProjectID, &m.Role, &m.Active)
	if errors.Is(err, sql.ErrNoRows) {
		return project.Member{}, project.ErrForbidden
	}
	return m, err
}

func (r *ProjectRepository) FindProjectByWriteKeyHash(ctx context.Context, hash string, now time.Time) (project.Project, error) {
	var p project.Project
	var previousHash sql.NullString
	var previousExpiry sql.NullTime
	var origins []byte
	err := r.db.QueryRowContext(ctx, `SELECT id,code,name,write_key_hash,previous_write_key_hash,previous_write_key_expires_at,allowed_origins,timezone,visitor_salt,is_active,created_by FROM project WHERE is_active=TRUE AND (write_key_hash=? OR (previous_write_key_hash=? AND previous_write_key_expires_at>?)) LIMIT 1`, hash, hash, now).Scan(&p.ID, &p.Code, &p.Name, &p.WriteKeyHash, &previousHash, &previousExpiry, &origins, &p.Timezone, &p.VisitorSalt, &p.Active, &p.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return project.Project{}, project.ErrProjectNotFound
	}
	if err != nil {
		return project.Project{}, err
	}
	p.PreviousWriteKeyHash = previousHash.String
	if previousExpiry.Valid {
		p.PreviousWriteKeyExpiresAt = &previousExpiry.Time
	}
	if err := json.Unmarshal(origins, &p.AllowedOrigins); err != nil {
		return project.Project{}, fmt.Errorf("decode project origins: %w", err)
	}
	return p, nil
}
func (r *ProjectRepository) RotateWriteKey(ctx context.Context, projectID uint64, hash string, now, expires time.Time) (err error) {
	return r.rotateWriteKey(ctx, projectID, 0, hash, now, expires, false)
}
func (r *ProjectRepository) RotateWriteKeyAudited(ctx context.Context, projectID, actor uint64, hash string, now, expires time.Time) error {
	return r.rotateWriteKey(ctx, projectID, actor, hash, now, expires, true)
}
func (r *ProjectRepository) rotateWriteKey(ctx context.Context, projectID, actor uint64, hash string, now, expires time.Time, audited bool) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var previousExpiry sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT previous_write_key_expires_at FROM project WHERE id=? AND is_active=TRUE FOR UPDATE`, projectID).Scan(&previousExpiry)
	if errors.Is(err, sql.ErrNoRows) {
		return project.ErrProjectNotFound
	}
	if err != nil {
		return err
	}
	if previousExpiry.Valid && previousExpiry.Time.After(now) {
		return project.ErrWriteKeyRotationInProgress
	}
	_, err = tx.ExecContext(ctx, `UPDATE project SET previous_write_key_hash=write_key_hash,previous_write_key_expires_at=?,write_key_hash=? WHERE id=?`, expires, hash, projectID)
	if err != nil {
		return err
	}
	if audited {
		if err = insertProjectAudit(ctx, tx, projectID, actor, "ROTATE_WRITE_KEY", "PROJECT", projectID, map[string]any{"previous_key_expires_at": previousExpiry.Time}, map[string]any{"previous_key_expires_at": expires}); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (r *ProjectRepository) ListProjects(ctx context.Context, userID uint64) ([]project.Project, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT p.id,p.code,p.name,p.allowed_origins,p.timezone,p.is_active,p.created_by,m.role FROM project p JOIN project_member m ON m.project_id=p.id WHERE m.user_id=? AND m.is_active=TRUE ORDER BY p.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []project.Project
	for rows.Next() {
		var p project.Project
		var origins []byte
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &origins, &p.Timezone, &p.Active, &p.CreatedBy, &p.Role); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(origins, &p.AllowedOrigins); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r *ProjectRepository) ListStatisticsProjects(ctx context.Context) ([]ports.StatisticsProject, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,timezone FROM project WHERE is_active=TRUE ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ports.StatisticsProject
	for rows.Next() {
		var p ports.StatisticsProject
		if err := rows.Scan(&p.ID, &p.Timezone); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r *ProjectRepository) ExpirePreviousWriteKeys(ctx context.Context, now time.Time) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE project SET previous_write_key_hash=NULL,previous_write_key_expires_at=NULL WHERE previous_write_key_expires_at IS NOT NULL AND previous_write_key_expires_at<=?`, now)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return affected, nil
}
func (r *ProjectRepository) UpdateProjectSettings(ctx context.Context, projectID, actor uint64, in project.SettingsInput) (out project.Project, err error) {
	origins, err := json.Marshal(nonNilStrings(in.AllowedOrigins))
	if err != nil {
		return out, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var beforeOrigins []byte
	if err = tx.QueryRowContext(ctx, `SELECT id,code,name,allowed_origins,timezone,is_active,created_by FROM project WHERE id=? AND is_active=TRUE FOR UPDATE`, projectID).Scan(&out.ID, &out.Code, &out.Name, &beforeOrigins, &out.Timezone, &out.Active, &out.CreatedBy); errors.Is(err, sql.ErrNoRows) {
		return out, project.ErrProjectNotFound
	} else if err != nil {
		return out, err
	}
	before := map[string]any{"timezone": out.Timezone, "allowed_origins": json.RawMessage(beforeOrigins)}
	if _, err = tx.ExecContext(ctx, `UPDATE project SET allowed_origins=?,timezone=? WHERE id=?`, origins, in.Timezone, projectID); err != nil {
		return out, err
	}
	after := map[string]any{"timezone": in.Timezone, "allowed_origins": json.RawMessage(origins)}
	if err = insertProjectAudit(ctx, tx, projectID, actor, "UPDATE_SETTINGS", "PROJECT", projectID, before, after); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	out.Timezone = in.Timezone
	out.AllowedOrigins = nonNilStrings(in.AllowedOrigins)
	return out, nil
}
func (r *ProjectRepository) ListProjectMembers(ctx context.Context, projectID uint64) ([]project.Member, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT m.user_id,m.project_id,u.username,u.display_name,m.role,m.is_active FROM project_member m JOIN user_account u ON u.id=m.user_id WHERE m.project_id=? AND m.is_active=TRUE ORDER BY u.username`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []project.Member{}
	for rows.Next() {
		var m project.Member
		if err := rows.Scan(&m.UserID, &m.ProjectID, &m.Username, &m.DisplayName, &m.Role, &m.Active); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (r *ProjectRepository) AddProjectMember(ctx context.Context, projectID, actor uint64, in project.MemberInput) (out project.Member, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = tx.QueryRowContext(ctx, `SELECT id,username,display_name FROM user_account WHERE username=? AND status='ACTIVE' LIMIT 1 FOR UPDATE`, in.Username).Scan(&out.UserID, &out.Username, &out.DisplayName); errors.Is(err, sql.ErrNoRows) {
		return out, project.ErrProjectNotFound
	} else if err != nil {
		return out, err
	}
	var oldRole sql.NullString
	var oldActive sql.NullBool
	err = tx.QueryRowContext(ctx, `SELECT role,is_active FROM project_member WHERE project_id=? AND user_id=? FOR UPDATE`, projectID, out.UserID).Scan(&oldRole, &oldActive)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if oldRole.Valid && project.Role(oldRole.String) == project.Admin && oldActive.Bool && in.Role == project.Viewer {
		var admins int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_member WHERE project_id=? AND role='ADMIN' AND is_active=TRUE FOR UPDATE`, projectID).Scan(&admins); err != nil {
			return out, err
		}
		if admins <= 1 {
			return out, project.ErrInvalidProject
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO project_member(project_id,user_id,role,is_active) VALUES(?,?,?,TRUE)`, projectID, out.UserID, in.Role)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE project_member SET role=?,is_active=TRUE WHERE project_id=? AND user_id=?`, in.Role, projectID, out.UserID)
	}
	if err != nil {
		return out, err
	}
	before := any(nil)
	if oldRole.Valid {
		before = map[string]any{"role": oldRole.String, "is_active": oldActive.Bool}
	}
	if err = insertProjectAudit(ctx, tx, projectID, actor, "UPSERT_MEMBER", "PROJECT_MEMBER", out.UserID, before, map[string]any{"role": in.Role, "is_active": true}); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	out.ProjectID = projectID
	out.Role = in.Role
	out.Active = true
	return out, nil
}
func (r *ProjectRepository) RemoveProjectMember(ctx context.Context, projectID, actor, userID uint64) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var role project.Role
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT role,is_active FROM project_member WHERE project_id=? AND user_id=? FOR UPDATE`, projectID, userID).Scan(&role, &active); errors.Is(err, sql.ErrNoRows) {
		return project.ErrProjectNotFound
	} else if err != nil {
		return err
	}
	if role == project.Admin && active {
		var admins int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_member WHERE project_id=? AND role='ADMIN' AND is_active=TRUE FOR UPDATE`, projectID).Scan(&admins); err != nil {
			return err
		}
		if admins <= 1 {
			return project.ErrInvalidProject
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE project_member SET is_active=FALSE WHERE project_id=? AND user_id=?`, projectID, userID); err != nil {
		return err
	}
	if err = insertProjectAudit(ctx, tx, projectID, actor, "REMOVE_MEMBER", "PROJECT_MEMBER", userID, map[string]any{"role": role, "is_active": active}, map[string]any{"role": role, "is_active": false}); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *ProjectRepository) WriteKeyStatus(ctx context.Context, projectID uint64) (project.WriteKeyStatus, error) {
	var expiry sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT previous_write_key_expires_at FROM project WHERE id=? AND is_active=TRUE`, projectID).Scan(&expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return project.WriteKeyStatus{}, project.ErrProjectNotFound
	}
	if err != nil {
		return project.WriteKeyStatus{}, err
	}
	out := project.WriteKeyStatus{}
	if expiry.Valid {
		out.PreviousKeyExpiresAt = &expiry.Time
	}
	return out, nil
}
func insertProjectAudit(ctx context.Context, tx *sql.Tx, projectID, actor uint64, action, resource string, resourceID uint64, before, after any) error {
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return err
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_log(project_id,actor_id,action,resource_type,resource_id,before_snapshot,after_snapshot) VALUES(?,?,?,?,?,?,?)`, projectID, actor, action, resource, resourceID, beforeJSON, afterJSON)
	return err
}
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
