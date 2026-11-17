package funnel

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/lzw7758/go-dot/internal/project"
)

var ErrInvalid = errors.New("invalid funnel query")
var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,127}$`)

type Authorizer interface {
	RequireRole(context.Context, uint64, uint64, ...project.Role) error
}
type Store interface {
	QueryFunnel(context.Context, Query) ([]uint64, error)
}
type Service struct {
	auth  Authorizer
	store Store
	now   func() time.Time
}

// NewService 创建漏斗分析服务。
func NewService(auth Authorizer, store Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{auth: auth, store: store, now: now}
}

type Query struct {
	ProjectID      uint64    `json:"project_id"`
	Start          time.Time `json:"start"`
	End            time.Time `json:"end"`
	WindowHours    int       `json:"window_hours"`
	PointCodes     []string  `json:"point_codes"`
	Platform       string    `json:"platform"`
	Channel        string    `json:"channel"`
	EvaluatedUntil time.Time `json:"-"`
}
type Step struct {
	PointCode  string   `json:"point_code"`
	Visitors   uint64   `json:"visitors"`
	Conversion *float64 `json:"conversion"`
}
type Result struct {
	Steps          []Step    `json:"steps"`
	Incomplete     bool      `json:"incomplete"`
	EvaluatedUntil time.Time `json:"evaluated_until"`
}

// Run 校验口径及权限并计算每一步的精确访客数。
func (s *Service) Run(ctx context.Context, actor uint64, q Query) (Result, error) {
	if err := s.auth.RequireRole(ctx, actor, q.ProjectID, project.Admin, project.Viewer); err != nil {
		return Result{}, err
	}
	now := s.now().UTC()
	q.Start = q.Start.UTC()
	q.End = q.End.UTC()
	if q.ProjectID == 0 || q.Start.IsZero() || !q.Start.Before(q.End) || q.End.After(now) || q.Start.Before(now.Add(-21*24*time.Hour)) || q.End.Sub(q.Start) > 7*24*time.Hour || q.WindowHours < 1 || q.WindowHours > 168 || len(q.PointCodes) < 2 || len(q.PointCodes) > 5 || len(q.Platform) > 32 || len(q.Channel) > 128 {
		return Result{}, ErrInvalid
	}
	seen := map[string]bool{}
	for _, code := range q.PointCodes {
		if !codePattern.MatchString(code) || seen[code] {
			return Result{}, ErrInvalid
		}
		seen[code] = true
	}
	end := q.End.Add(time.Duration(q.WindowHours) * time.Hour)
	incomplete := end.After(now)
	if incomplete {
		end = now
	}
	q.EvaluatedUntil = end
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	values, err := s.store.QueryFunnel(queryCtx, q)
	if err != nil {
		return Result{}, err
	}
	if len(values) != len(q.PointCodes) {
		return Result{}, errors.New("invalid funnel result")
	}
	for i := 1; i < len(values); i++ {
		if values[i] > values[i-1] {
			return Result{}, errors.New("invalid funnel counts")
		}
	}
	out := Result{Steps: make([]Step, len(values)), Incomplete: incomplete, EvaluatedUntil: end}
	for i, count := range values {
		out.Steps[i] = Step{PointCode: q.PointCodes[i], Visitors: count}
		if i > 0 && values[i-1] > 0 {
			v := float64(count) / float64(values[i-1])
			out.Steps[i].Conversion = &v
		}
	}
	return out, nil
}
