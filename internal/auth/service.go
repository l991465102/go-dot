package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Repository interface {
	CreateInitialUser(context.Context, User) (User, error)
	FindUserByUsername(context.Context, string) (User, error)
	FindUserByID(context.Context, uint64) (User, error)
	UpdatePassword(context.Context, uint64, string) error
	TouchLogin(context.Context, uint64, time.Time) error
	StoreRefreshToken(context.Context, RefreshToken) error
	ConsumeRefreshToken(context.Context, string, time.Time) (RefreshToken, error)
	RevokeRefreshToken(context.Context, string, time.Time) error
}

type Service struct {
	repo                  Repository
	jwtSecret             []byte
	accessTTL, refreshTTL time.Duration
	now                   func() time.Time
}

func NewService(repo Repository, jwtSecret []byte, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{repo: repo, jwtSecret: append([]byte(nil), jwtSecret...), accessTTL: accessTTL, refreshTTL: refreshTTL, now: time.Now}
}

func (s *Service) EnsureInitialAdmin(ctx context.Context, username, password string) error {
	if username == "" || password == "" {
		return errors.New("initial username and password are required")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.repo.CreateInitialUser(ctx, User{Username: username, DisplayName: username, PasswordHash: hash, MustChangePassword: true, Status: "ACTIVE"})
	return err
}

func (s *Service) Login(ctx context.Context, username, password string) (Session, error) {
	user, err := s.repo.FindUserByUsername(ctx, username)
	if err != nil || user.Status != "ACTIVE" || !CheckPassword(password, user.PasswordHash) {
		return Session{}, ErrInvalidCredentials
	}
	now := s.now().UTC()
	if err := s.repo.TouchLogin(ctx, user.ID, now); err != nil {
		return Session{}, fmt.Errorf("record login: %w", err)
	}
	return s.issueSession(ctx, user, now)
}

func (s *Service) ChangePassword(ctx context.Context, userID uint64, current, next string) error {
	user, err := s.repo.FindUserByID(ctx, userID)
	if err != nil {
		return ErrInvalidCredentials
	}
	if !CheckPassword(current, user.PasswordHash) {
		return ErrInvalidCredentials
	}
	if next == "" || next == current {
		return errors.New("new password must be non-empty and different")
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	return s.repo.UpdatePassword(ctx, userID, hash)
}

func (s *Service) Refresh(ctx context.Context, raw string) (Session, error) {
	now := s.now().UTC()
	token, err := s.repo.ConsumeRefreshToken(ctx, tokenHash(raw), now)
	if err != nil {
		return Session{}, ErrInvalidToken
	}
	user, err := s.repo.FindUserByID(ctx, token.UserID)
	if err != nil || user.Status != "ACTIVE" {
		return Session{}, ErrInvalidToken
	}
	return s.issueSession(ctx, user, now)
}

func (s *Service) Logout(ctx context.Context, raw string) error {
	if raw == "" {
		return ErrInvalidToken
	}
	return s.repo.RevokeRefreshToken(ctx, tokenHash(raw), s.now().UTC())
}

func (s *Service) ParseAccessToken(raw string) (Principal, error) {
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, ErrInvalidToken
		}
		return s.jwtSecret, nil
	}, jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || !token.Valid {
		return Principal{}, ErrInvalidToken
	}
	sub, err := claims.GetSubject()
	if err != nil {
		return Principal{}, ErrInvalidToken
	}
	id, err := strconv.ParseUint(sub, 10, 64)
	if err != nil {
		return Principal{}, ErrInvalidToken
	}
	username, _ := claims["username"].(string)
	mustChange, _ := claims["must_change_password"].(bool)
	return Principal{UserID: id, Username: username, MustChangePassword: mustChange}, nil
}

func (s *Service) User(ctx context.Context, id uint64) (User, error) {
	return s.repo.FindUserByID(ctx, id)
}

func (s *Service) issueSession(ctx context.Context, user User, now time.Time) (Session, error) {
	expires := now.Add(s.accessTTL)
	claims := jwt.MapClaims{"sub": strconv.FormatUint(user.ID, 10), "username": user.Username, "must_change_password": user.MustChangePassword, "iat": now.Unix(), "exp": expires.Unix()}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
	if err != nil {
		return Session{}, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Session{}, fmt.Errorf("generate refresh token: %w", err)
	}
	refresh := base64.RawURLEncoding.EncodeToString(raw)
	if err := s.repo.StoreRefreshToken(ctx, RefreshToken{UserID: user.ID, Hash: tokenHash(refresh), ExpiresAt: now.Add(s.refreshTTL)}); err != nil {
		return Session{}, fmt.Errorf("store refresh token: %w", err)
	}
	user.PasswordHash = ""
	return Session{AccessToken: access, RefreshToken: refresh, ExpiresIn: int64(s.accessTTL.Seconds()), MustChangePassword: user.MustChangePassword, User: user}, nil
}

func tokenHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
