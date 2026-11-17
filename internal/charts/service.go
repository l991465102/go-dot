package charts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lzw7758/go-dot/internal/project"
)

var ErrInvalid = errors.New("invalid chart")
var ErrNotFound = errors.New("chart not found")
var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,127}$`)

type Authorizer interface {
	RequireRole(context.Context, uint64, uint64, ...project.Role) error
}
type Service struct {
	db   *sql.DB
	auth Authorizer
}

// NewService 创建已保存图表服务。
func NewService(db *sql.DB, auth Authorizer) *Service { return &Service{db: db, auth: auth} }

type Config struct {
	PointCodes  []string `json:"point_codes"`
	Start       string   `json:"start"`
	End         string   `json:"end"`
	WindowHours int      `json:"window_hours"`
	Metric      string   `json:"metric"`
	Granularity string   `json:"granularity"`
	Timezone    string   `json:"timezone"`
	Platform    string   `json:"platform"`
	Channel     string   `json:"channel"`
	Domain      string   `json:"domain"`
	Module      string   `json:"module"`
	Scene       string   `json:"scene"`
}
type Input struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Config Config `json:"config"`
}
type Chart struct {
	ID        uint64    `json:"id"`
	ProjectID uint64    `json:"project_id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Config    Config    `json:"config"`
	CreatedBy uint64    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// Validate 校验图表名称、点位与查询参数。
func (in Input) Validate() error {
	if strings.TrimSpace(in.Name) == "" || utf8.RuneCountInString(in.Name) > 128 || (in.Kind != "FUNNEL" && in.Kind != "TREND") || len(in.Config.PointCodes) < 2 || len(in.Config.PointCodes) > 5 || len(in.Config.Platform) > 32 || len(in.Config.Channel) > 128 || len(in.Config.Domain) > 128 || len(in.Config.Module) > 128 || len(in.Config.Scene) > 128 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, c := range in.Config.PointCodes {
		if !codePattern.MatchString(c) || seen[c] {
			return ErrInvalid
		}
		seen[c] = true
	}
	start, e := time.Parse(time.RFC3339, in.Config.Start)
	if e != nil {
		return ErrInvalid
	}
	end, e := time.Parse(time.RFC3339, in.Config.End)
	if e != nil || !start.Before(end) {
		return ErrInvalid
	}
	if in.Kind == "FUNNEL" {
		if in.Config.WindowHours < 1 || in.Config.WindowHours > 168 || end.Sub(start) > 7*24*time.Hour {
			return ErrInvalid
		}
	} else {
		if (in.Config.Metric != "pv" && in.Config.Metric != "uv") || (in.Config.Granularity != "hour" && in.Config.Granularity != "day") || end.Sub(start) > 31*24*time.Hour {
			return ErrInvalid
		}
		if _, e = time.LoadLocation(in.Config.Timezone); e != nil {
			return ErrInvalid
		}
	}
	return nil
}

// List 返回项目内同类型的已保存图表。
func (s *Service) List(ctx context.Context, actor, projectID uint64, kind string) ([]Chart, error) {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin, project.Viewer); err != nil {
		return nil, err
	}
	if kind != "TREND" && kind != "FUNNEL" {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,name,kind,config,created_by,created_at FROM saved_chart WHERE project_id=? AND kind=? ORDER BY id DESC LIMIT 200`, projectID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Chart{}
	for rows.Next() {
		var c Chart
		var raw []byte
		if err = rows.Scan(&c.ID, &c.ProjectID, &c.Name, &c.Kind, &raw, &c.CreatedBy, &c.CreatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &c.Config); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

// Create 保存图表配置及审计记录。
func (s *Service) Create(ctx context.Context, actor, projectID uint64, in Input) (uint64, error) {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin); err != nil {
		return 0, err
	}
	if err := in.Validate(); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, code := range in.Config.PointCodes {
		var id uint64
		err = tx.QueryRowContext(ctx, `SELECT id FROM tracking_point WHERE project_id=? AND code=? AND definition_state IN ('PUBLISHED','DEPRECATED')`, projectID, code).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrInvalid
		}
		if err != nil {
			return 0, err
		}
	}
	data, err := json.Marshal(in.Config)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO saved_chart (project_id,name,kind,config,created_by) VALUES (?,?,?,?,?)`, projectID, strings.TrimSpace(in.Name), in.Kind, string(data), actor)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	audit, err := json.Marshal(in)
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_log (project_id,actor_id,action,resource_type,resource_id,after_snapshot) VALUES (?,?,?,?,?,?)`, projectID, actor, "CREATE", "saved_chart", id, string(audit)); err != nil {
		return 0, err
	}
	return uint64(id), tx.Commit()
}

// Delete 删除项目内已保存图表并记录审计。
func (s *Service) Delete(ctx context.Context, actor, projectID, id uint64) error {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var name, kind string
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT name,kind,config FROM saved_chart WHERE id=? AND project_id=? FOR UPDATE`, id, projectID).Scan(&name, &kind, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	snapshot, err := json.Marshal(struct {
		Name   string          `json:"name"`
		Kind   string          `json:"kind"`
		Config json.RawMessage `json:"config"`
	}{name, kind, raw})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_log (project_id,actor_id,action,resource_type,resource_id,before_snapshot) VALUES (?,?,?,?,?,?)`, projectID, actor, "DELETE", "saved_chart", id, string(snapshot)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM saved_chart WHERE id=? AND project_id=?`, id, projectID); err != nil {
		return err
	}
	return tx.Commit()
}
