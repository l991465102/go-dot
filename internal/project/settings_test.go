package project

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSettingsRequireAdminAndDelegateAuditedMutation(t *testing.T) {
	repo := &settingsRepo{member: Member{UserID: 1, ProjectID: 9, Role: Admin, Active: true}}
	svc := NewService(repo)
	got, err := svc.UpdateSettings(context.Background(), 1, 9, SettingsInput{Timezone: "UTC", AllowedOrigins: []string{"https://example.com"}})
	require.NoError(t, err)
	require.Equal(t, "UTC", got.Timezone)
	require.Equal(t, uint64(1), repo.settingsActor)

	repo.member.Role = Viewer
	_, err = svc.UpdateSettings(context.Background(), 1, 9, SettingsInput{Timezone: "UTC"})
	require.ErrorIs(t, err, ErrForbidden)
}

func TestMembersAreReadableByViewerButMutableByAdmin(t *testing.T) {
	repo := &settingsRepo{member: Member{UserID: 2, ProjectID: 9, Role: Viewer, Active: true}, members: []Member{{UserID: 2, ProjectID: 9, Username: "reader", Role: Viewer, Active: true}}}
	svc := NewService(repo)
	items, err := svc.ListMembers(context.Background(), 2, 9)
	require.NoError(t, err)
	require.Equal(t, "reader", items[0].Username)
	require.ErrorIs(t, svc.AddMember(context.Background(), 2, 9, MemberInput{Username: "admin", Role: Admin}), ErrForbidden)
}

func TestSettingsRejectInvalidTimezoneAndNonOriginURL(t *testing.T) {
	require.ErrorIs(t, (SettingsInput{Timezone: "Missing/Timezone"}).Validate(), ErrInvalidProject)
	require.ErrorIs(t, (SettingsInput{Timezone: "UTC", AllowedOrigins: []string{"https://example.com/path"}}).Validate(), ErrInvalidProject)
	require.NoError(t, (SettingsInput{Timezone: "UTC", AllowedOrigins: []string{"https://example.com"}}).Validate())
}

type settingsRepo struct {
	member        Member
	members       []Member
	settingsActor uint64
}

func (r *settingsRepo) CreateProject(context.Context, Project, Member) (Project, error) {
	return Project{}, nil
}
func (r *settingsRepo) FindMember(context.Context, uint64, uint64) (Member, error) {
	return r.member, nil
}
func (r *settingsRepo) RotateWriteKey(context.Context, uint64, string, time.Time, time.Time) error {
	return nil
}
func (r *settingsRepo) ListProjects(context.Context, uint64) ([]Project, error) { return nil, nil }
func (r *settingsRepo) UpdateProjectSettings(_ context.Context, projectID, actor uint64, in SettingsInput) (Project, error) {
	r.settingsActor = actor
	return Project{ID: projectID, Timezone: in.Timezone, AllowedOrigins: in.AllowedOrigins}, nil
}
func (r *settingsRepo) ListProjectMembers(context.Context, uint64) ([]Member, error) {
	return r.members, nil
}
func (r *settingsRepo) AddProjectMember(context.Context, uint64, uint64, MemberInput) (Member, error) {
	return Member{}, nil
}
func (r *settingsRepo) RemoveProjectMember(context.Context, uint64, uint64, uint64) error { return nil }
func (r *settingsRepo) WriteKeyStatus(context.Context, uint64) (WriteKeyStatus, error) {
	return WriteKeyStatus{}, nil
}
