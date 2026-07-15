package auth

import (
	"errors"
	"time"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid token")
	ErrUserNotFound       = errors.New("user not found")
)

type User struct {
	ID                 uint64    `json:"id"`
	Username           string    `json:"username"`
	DisplayName        string    `json:"display_name"`
	PasswordHash       string    `json:"-"`
	MustChangePassword bool      `json:"must_change_password"`
	Status             string    `json:"status"`
	LastLoginAt        time.Time `json:"last_login_at,omitempty"`
}

type RefreshToken struct {
	UserID    uint64
	Hash      string
	ExpiresAt time.Time
	RevokedAt *time.Time
}

type Session struct {
	AccessToken        string `json:"access_token"`
	RefreshToken       string `json:"-"`
	ExpiresIn          int64  `json:"expires_in"`
	MustChangePassword bool   `json:"must_change_password"`
	User               User   `json:"user"`
}

type Principal struct {
	UserID             uint64
	Username           string
	MustChangePassword bool
}
