package requirements

import (
	"context"
	"errors"
	"testing"

	"github.com/lzw7758/go-dot/internal/project"
)

type deny struct{}

func (deny) RequireRole(context.Context, uint64, uint64, ...project.Role) error {
	return project.ErrForbidden
}

func TestInputValidation(t *testing.T) {
	cases := []struct {
		in    Input
		valid bool
	}{
		{Input{Title: "转化分析", URL: "https://example.com/spec", DueDate: "2026-11-30"}, true},
		{Input{Title: "  "}, false}, {Input{Title: "x", DueDate: "2026-02-30"}, false},
		{Input{Title: "x", URL: "javascript:alert(1)"}, false}, {Input{Title: "x", URL: "https:///path"}, false},
	}
	for _, tc := range cases {
		if got := tc.in.Validate() == nil; got != tc.valid {
			t.Errorf("Validate(%+v) = %v", tc.in, got)
		}
	}
}
func TestTaskValidation(t *testing.T) {
	for _, tc := range []struct {
		in    TaskInput
		valid bool
	}{
		{TaskInput{PointID: 1, Platform: "web", State: "PENDING"}, true},
		{TaskInput{PointID: 0, Platform: "web", State: "PENDING"}, false},
		{TaskInput{PointID: 1, Platform: "unknown", State: "PENDING"}, false},
		{TaskInput{PointID: 1, Platform: "server", State: "DONE"}, false},
	} {
		if got := tc.in.Validate() == nil; got != tc.valid {
			t.Errorf("Validate(%+v) = %v", tc.in, got)
		}
	}
}
func TestAuthorizationPrecedesDatabaseAccess(t *testing.T) {
	s := NewService(nil, deny{})
	ctx := context.Background()
	if _, err := s.Create(ctx, 1, 2, Input{Title: "test"}); !errors.Is(err, project.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err := s.List(ctx, 1, 2); !errors.Is(err, project.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err := s.Tasks(ctx, 1, 2, 3); !errors.Is(err, project.ErrForbidden) {
		t.Fatal(err)
	}
	if err := s.SaveTask(ctx, 1, 2, 3, 0, TaskInput{}); !errors.Is(err, project.ErrForbidden) {
		t.Fatal(err)
	}
}

type captureRoles struct{ roles []project.Role }

func (a *captureRoles) RequireRole(_ context.Context, _, _ uint64, roles ...project.Role) error {
	a.roles = roles
	return project.ErrForbidden
}

// TestRolePolicy 确认需求和验收的读写接口使用预期项目角色。
func TestRolePolicy(t *testing.T) {
	a := &captureRoles{}
	s := NewService(nil, a)
	ctx := context.Background()
	read := func() {
		t.Helper()
		if len(a.roles) != 2 || a.roles[0] != project.Admin || a.roles[1] != project.Viewer {
			t.Fatalf("read roles=%v", a.roles)
		}
	}
	write := func() {
		t.Helper()
		if len(a.roles) != 1 || a.roles[0] != project.Admin {
			t.Fatalf("write roles=%v", a.roles)
		}
	}
	s.List(ctx, 1, 2)
	read()
	s.Tasks(ctx, 1, 2, 3)
	read()
	s.ListAcceptances(ctx, 1, 2, 3, 4)
	read()
	s.Create(ctx, 1, 2, Input{})
	write()
	s.Update(ctx, 1, 2, 3, Input{})
	write()
	s.SaveTask(ctx, 1, 2, 3, 4, TaskInput{})
	write()
	s.DeleteTask(ctx, 1, 2, 3, 4)
	write()
	s.AddAcceptance(ctx, 1, 2, 3, 4, AcceptanceInput{})
	write()
}
