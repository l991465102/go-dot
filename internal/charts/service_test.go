package charts

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

// TestValidation 检查图表类型、点位及查询区间。
func TestValidation(t *testing.T) {
	good := Input{Name: "转化", Kind: "FUNNEL", Config: Config{PointCodes: []string{"page_view", "order_submit"}, Start: "2026-01-01T00:00:00Z", End: "2026-01-02T00:00:00Z", WindowHours: 24}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	duplicate := good
	duplicate.Config.PointCodes = []string{"page_view", "page_view"}
	if !errors.Is(duplicate.Validate(), ErrInvalid) {
		t.Fatal("duplicate must fail")
	}
	invalid := good
	invalid.Config.WindowHours = 0
	if !errors.Is(invalid.Validate(), ErrInvalid) {
		t.Fatal("window must fail")
	}
	trend := good
	trend.Kind = "TREND"
	trend.Config.Metric = "uv"
	trend.Config.Granularity = "hour"
	trend.Config.Timezone = "Asia/Shanghai"
	if err := trend.Validate(); err != nil {
		t.Fatal(err)
	}
}

// TestAuthorizationBeforeDatabase 确认未授权请求不会访问数据库。
func TestAuthorizationBeforeDatabase(t *testing.T) {
	s := NewService(nil, deny{})
	ctx := context.Background()
	if _, err := s.List(ctx, 1, 2, "TREND"); !errors.Is(err, project.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, 1, 2, Input{}); !errors.Is(err, project.ErrForbidden) {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, 1, 2, 3); !errors.Is(err, project.ErrForbidden) {
		t.Fatal(err)
	}
}
