package project

import (
	"errors"
	"net/url"
	"strings"
	"time"
)

var (
	ErrForbidden                  = errors.New("forbidden")
	ErrInvalidProject             = errors.New("invalid project")
	ErrProjectNotFound            = errors.New("project not found")
	ErrWriteKeyRotationInProgress = errors.New("write key rotation already in progress")
)

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
	Role                      Role       `json:"role,omitempty"`
}

type Member struct {
	UserID      uint64 `json:"user_id"`
	ProjectID   uint64 `json:"project_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	Role        Role   `json:"role"`
	Active      bool   `json:"is_active"`
}
type SettingsInput struct {
	AllowedOrigins []string `json:"allowed_origins"`
	Timezone       string   `json:"timezone"`
}
type MemberInput struct {
	Username string `json:"username"`
	Role     Role   `json:"role"`
}
type WriteKeyStatus struct {
	PreviousKeyExpiresAt *time.Time `json:"previous_key_expires_at,omitempty"`
	PreviousKeyActive    bool       `json:"previous_key_active"`
}

func (in SettingsInput) Validate() error {
	if _, err := time.LoadLocation(in.Timezone); in.Timezone == "" || err != nil {
		return ErrInvalidProject
	}
	seen := map[string]struct{}{}
	if len(in.AllowedOrigins) > 100 {
		return ErrInvalidProject
	}
	for _, origin := range in.AllowedOrigins {
		if origin != strings.TrimSpace(origin) || len(origin) > 2048 {
			return ErrInvalidProject
		}
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return ErrInvalidProject
		}
		if _, exists := seen[origin]; exists {
			return ErrInvalidProject
		}
		seen[origin] = struct{}{}
	}
	return nil
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
