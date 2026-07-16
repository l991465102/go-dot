package project

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Repository interface {
	CreateProject(context.Context, Project, Member) (Project, error)
	FindMember(context.Context, uint64, uint64) (Member, error)
	RotateWriteKey(context.Context, uint64, string, time.Time, time.Time) error
	ListProjects(context.Context, uint64) ([]Project, error)
}

type SettingsRepository interface {
	UpdateProjectSettings(context.Context, uint64, uint64, SettingsInput) (Project, error)
	ListProjectMembers(context.Context, uint64) ([]Member, error)
	AddProjectMember(context.Context, uint64, uint64, MemberInput) (Member, error)
	RemoveProjectMember(context.Context, uint64, uint64, uint64) error
	WriteKeyStatus(context.Context, uint64) (WriteKeyStatus, error)
}
type AuditedWriteKeyRepository interface {
	RotateWriteKeyAudited(context.Context, uint64, uint64, string, time.Time, time.Time) error
}

type Service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository) *Service { return &Service{repo: repo, now: time.Now} }

func (s *Service) RequireRole(ctx context.Context, userID, projectID uint64, allowed ...Role) error {
	m, err := s.repo.FindMember(ctx, userID, projectID)
	if errors.Is(err, ErrForbidden) {
		return ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("find project member: %w", err)
	}
	if !m.Active {
		return ErrForbidden
	}
	for _, role := range allowed {
		if m.Role == role {
			return nil
		}
	}
	return ErrForbidden
}

func (s *Service) Create(ctx context.Context, actorID uint64, in CreateInput) (Creation, error) {
	if in.Code == "" || in.Name == "" {
		return Creation{}, fmt.Errorf("%w: project code and name are required", ErrInvalidProject)
	}
	if in.Timezone == "" {
		in.Timezone = "Asia/Shanghai"
	}
	key, hash, err := newSecret()
	if err != nil {
		return Creation{}, err
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return Creation{}, err
	}
	p, err := s.repo.CreateProject(ctx, Project{Code: in.Code, Name: in.Name, WriteKeyHash: hash, AllowedOrigins: in.AllowedOrigins, Timezone: in.Timezone, VisitorSalt: salt, Active: true, CreatedBy: actorID}, Member{UserID: actorID, Role: Admin, Active: true})
	if err != nil {
		return Creation{}, err
	}
	p.Role = Admin
	return Creation{Project: p, WriteKey: key}, nil
}

func (s *Service) RotateWriteKey(ctx context.Context, actorID, projectID uint64) (WriteKeyRotation, error) {
	if err := s.RequireRole(ctx, actorID, projectID, Admin); err != nil {
		return WriteKeyRotation{}, err
	}
	key, hash, err := newSecret()
	if err != nil {
		return WriteKeyRotation{}, err
	}
	now := s.now().UTC()
	expires := now.Add(24 * time.Hour)
	var rotateErr error
	if audited, ok := s.repo.(AuditedWriteKeyRepository); ok {
		rotateErr = audited.RotateWriteKeyAudited(ctx, projectID, actorID, hash, now, expires)
	} else {
		rotateErr = s.repo.RotateWriteKey(ctx, projectID, hash, now, expires)
	}
	if rotateErr != nil {
		return WriteKeyRotation{}, rotateErr
	}
	return WriteKeyRotation{WriteKey: key, PreviousKeyExpiresAt: expires}, nil
}

func (s *Service) List(ctx context.Context, userID uint64) ([]Project, error) {
	return s.repo.ListProjects(ctx, userID)
}

func (s *Service) ProjectTimezone(ctx context.Context, userID, projectID uint64) (string, error) {
	projects, err := s.repo.ListProjects(ctx, userID)
	if err != nil {
		return "", err
	}
	for _, item := range projects {
		if item.ID == projectID && item.Active {
			return item.Timezone, nil
		}
	}
	return "", ErrForbidden
}

func (s *Service) settingsRepository() (SettingsRepository, error) {
	repo, ok := s.repo.(SettingsRepository)
	if !ok {
		return nil, errors.New("project settings repository unavailable")
	}
	return repo, nil
}
func (s *Service) UpdateSettings(ctx context.Context, actor, projectID uint64, in SettingsInput) (Project, error) {
	if err := s.RequireRole(ctx, actor, projectID, Admin); err != nil {
		return Project{}, err
	}
	if err := in.Validate(); err != nil {
		return Project{}, err
	}
	repo, err := s.settingsRepository()
	if err != nil {
		return Project{}, err
	}
	return repo.UpdateProjectSettings(ctx, projectID, actor, in)
}
func (s *Service) ListMembers(ctx context.Context, actor, projectID uint64) ([]Member, error) {
	if err := s.RequireRole(ctx, actor, projectID, Admin, Viewer); err != nil {
		return nil, err
	}
	repo, err := s.settingsRepository()
	if err != nil {
		return nil, err
	}
	return repo.ListProjectMembers(ctx, projectID)
}
func (s *Service) AddMember(ctx context.Context, actor, projectID uint64, in MemberInput) error {
	if err := s.RequireRole(ctx, actor, projectID, Admin); err != nil {
		return err
	}
	if in.Username == "" || in.Username != strings.TrimSpace(in.Username) || len(in.Username) > 128 || (in.Role != Admin && in.Role != Viewer) {
		return ErrInvalidProject
	}
	repo, err := s.settingsRepository()
	if err != nil {
		return err
	}
	_, err = repo.AddProjectMember(ctx, projectID, actor, in)
	return err
}
func (s *Service) RemoveMember(ctx context.Context, actor, projectID, userID uint64) error {
	if err := s.RequireRole(ctx, actor, projectID, Admin); err != nil {
		return err
	}
	if actor == userID {
		return ErrInvalidProject
	}
	repo, err := s.settingsRepository()
	if err != nil {
		return err
	}
	return repo.RemoveProjectMember(ctx, projectID, actor, userID)
}
func (s *Service) GetWriteKeyStatus(ctx context.Context, actor, projectID uint64) (WriteKeyStatus, error) {
	if err := s.RequireRole(ctx, actor, projectID, Admin, Viewer); err != nil {
		return WriteKeyStatus{}, err
	}
	repo, err := s.settingsRepository()
	if err != nil {
		return WriteKeyStatus{}, err
	}
	status, err := repo.WriteKeyStatus(ctx, projectID)
	if err != nil {
		return WriteKeyStatus{}, err
	}
	status.PreviousKeyActive = status.PreviousKeyExpiresAt != nil && status.PreviousKeyExpiresAt.After(s.now().UTC())
	return status, nil
}

func newSecret() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	key := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(key))
	return key, hex.EncodeToString(sum[:]), nil
}
