package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lzw7758/go-dot/internal/auth"
	"github.com/lzw7758/go-dot/internal/ports"
)

var _ ports.AuthRepository = (*AuthRepository)(nil)

type AuthRepository struct{ db *sql.DB }

func NewAuthRepository(db *sql.DB) *AuthRepository { return &AuthRepository{db: db} }

func (r *AuthRepository) CreateInitialUser(ctx context.Context, user auth.User) (auth.User, error) {
	_, err := r.db.ExecContext(ctx, `INSERT INTO user_account(username,password_hash,display_name,must_change_password,status)
VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE id=id`, user.Username, user.PasswordHash, user.DisplayName, user.MustChangePassword, user.Status)
	if err != nil {
		return auth.User{}, fmt.Errorf("create initial user: %w", err)
	}
	return r.FindUserByUsername(ctx, user.Username)
}
func (r *AuthRepository) FindUserByUsername(ctx context.Context, username string) (auth.User, error) {
	return scanUser(r.db.QueryRowContext(ctx, `SELECT id,username,password_hash,display_name,must_change_password,status,last_login_at FROM user_account WHERE username=? LIMIT 1`, username))
}
func (r *AuthRepository) FindUserByID(ctx context.Context, id uint64) (auth.User, error) {
	return scanUser(r.db.QueryRowContext(ctx, `SELECT id,username,password_hash,display_name,must_change_password,status,last_login_at FROM user_account WHERE id=? LIMIT 1`, id))
}
func scanUser(row *sql.Row) (auth.User, error) {
	var u auth.User
	var last sql.NullTime
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.MustChangePassword, &u.Status, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, auth.ErrUserNotFound
	}
	if err != nil {
		return auth.User{}, err
	}
	if last.Valid {
		u.LastLoginAt = last.Time
	}
	return u, nil
}
func (r *AuthRepository) UpdatePassword(ctx context.Context, id uint64, hash string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE user_account SET password_hash=?,must_change_password=FALSE WHERE id=?`, hash, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return auth.ErrUserNotFound
	}
	return nil
}
func (r *AuthRepository) TouchLogin(ctx context.Context, id uint64, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE user_account SET last_login_at=? WHERE id=?`, at, id)
	return err
}
func (r *AuthRepository) StoreRefreshToken(ctx context.Context, t auth.RefreshToken) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO refresh_token(user_id,token_hash,expires_at) VALUES(?,?,?)`, t.UserID, t.Hash, t.ExpiresAt)
	return err
}
func (r *AuthRepository) ConsumeRefreshToken(ctx context.Context, hash string, now time.Time) (token auth.RefreshToken, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return token, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var revoked sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT user_id,token_hash,expires_at,revoked_at FROM refresh_token WHERE token_hash=? FOR UPDATE`, hash).Scan(&token.UserID, &token.Hash, &token.ExpiresAt, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return token, auth.ErrInvalidToken
	}
	if err != nil {
		return token, err
	}
	if revoked.Valid || !token.ExpiresAt.After(now) {
		return token, auth.ErrInvalidToken
	}
	if _, err = tx.ExecContext(ctx, `UPDATE refresh_token SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL`, now, hash); err != nil {
		return token, err
	}
	if err = tx.Commit(); err != nil {
		return token, err
	}
	token.RevokedAt = &now
	return token, nil
}
func (r *AuthRepository) RevokeRefreshToken(ctx context.Context, hash string, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE refresh_token SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL`, now, hash)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return auth.ErrInvalidToken
	}
	return nil
}
