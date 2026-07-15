package project

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"
)

type Repository interface {
	CreateProject(context.Context, Project, Member) (Project, error)
	FindMember(context.Context, uint64, uint64) (Member, error)
	RotateWriteKey(context.Context, uint64, string, time.Time) error
	ListProjects(context.Context, uint64) ([]Project, error)
}

type Service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository) *Service { return &Service{repo: repo, now: time.Now} }

func (s *Service) RequireRole(ctx context.Context, userID, projectID uint64, allowed ...Role) error {
	m, err := s.repo.FindMember(ctx, userID, projectID)
	if err != nil || !m.Active {
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
		return Creation{}, errors.New("project code and name are required")
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
	expires := s.now().UTC().Add(24 * time.Hour)
	if err := s.repo.RotateWriteKey(ctx, projectID, hash, expires); err != nil {
		return WriteKeyRotation{}, err
	}
	return WriteKeyRotation{WriteKey: key, PreviousKeyExpiresAt: expires}, nil
}

func (s *Service) List(ctx context.Context, userID uint64) ([]Project, error) {
	return s.repo.ListProjects(ctx, userID)
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
