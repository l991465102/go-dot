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
	return tx.Commit()
}
func (r *ProjectRepository) ListProjects(ctx context.Context, userID uint64) ([]project.Project, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT p.id,p.code,p.name,p.allowed_origins,p.timezone,p.is_active,p.created_by FROM project p JOIN project_member m ON m.project_id=p.id WHERE m.user_id=? AND m.is_active=TRUE ORDER BY p.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []project.Project
	for rows.Next() {
		var p project.Project
		var origins []byte
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &origins, &p.Timezone, &p.Active, &p.CreatedBy); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(origins, &p.AllowedOrigins); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
