package project

import (
	"errors"
	"time"
)

var ErrForbidden = errors.New("forbidden")

type Role string

const (
	Admin  Role = "ADMIN"
	Viewer Role = "VIEWER"
)

type Project struct {
	ID                        uint64     `json:"id"`
	Code                      string     `json:"code"`
	Name                      string     `json:"name"`
	WriteKeyHash              string     `json:"-"`
	PreviousWriteKeyHash      string     `json:"-"`
	PreviousWriteKeyExpiresAt *time.Time `json:"-"`
	AllowedOrigins            []string   `json:"allowed_origins"`
	Timezone                  string     `json:"timezone"`
	VisitorSalt               []byte     `json:"-"`
	Active                    bool       `json:"is_active"`
	CreatedBy                 uint64     `json:"created_by"`
}

type Member struct {
	UserID    uint64 `json:"user_id"`
	ProjectID uint64 `json:"project_id"`
	Role      Role   `json:"role"`
	Active    bool   `json:"is_active"`
}
type CreateInput struct {
	Code           string   `json:"code"`
	Name           string   `json:"name"`
	AllowedOrigins []string `json:"allowed_origins"`
	Timezone       string   `json:"timezone"`
}
type Creation struct {
	Project  Project `json:"project"`
	WriteKey string  `json:"write_key"`
}
type WriteKeyRotation struct {
	WriteKey             string    `json:"write_key"`
	PreviousKeyExpiresAt time.Time `json:"previous_key_expires_at"`
}
