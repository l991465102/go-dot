package requirements

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lzw7758/go-dot/internal/project"
)

var ErrInvalid = errors.New("invalid requirement or task")
var ErrNotFound = errors.New("requirement or task not found")
var ErrConflict = errors.New("task point and platform cannot be changed")

type Authorizer interface {
	RequireRole(context.Context, uint64, uint64, ...project.Role) error
}
type Service struct {
	db   *sql.DB
	auth Authorizer
}

// NewService 创建需求协作服务。
func NewService(db *sql.DB, auth Authorizer) *Service { return &Service{db: db, auth: auth} }

type Requirement struct {
	ID        uint64    `json:"id"`
	ProjectID uint64    `json:"project_id"`
	Title     string    `json:"title"`
	Goal      string    `json:"goal"`
	Scenario  string    `json:"scenario"`
	URL       string    `json:"requirement_url"`
	DueDate   string    `json:"due_date"`
	CreatedBy uint64    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	Tasks     []Task    `json:"tasks,omitempty"`
}
type Task struct {
	ID            uint64  `json:"id"`
	RequirementID uint64  `json:"requirement_id"`
	PointID       uint64  `json:"point_id"`
	PointName     string  `json:"point_name"`
	Platform      string  `json:"platform"`
	AssigneeID    *uint64 `json:"assignee_id"`
	State         string  `json:"state"`
}
type Input struct {
	Title    string `json:"title"`
	Goal     string `json:"goal"`
	Scenario string `json:"scenario"`
	URL      string `json:"requirement_url"`
	DueDate  string `json:"due_date"`
}
type TaskInput struct {
	PointID    uint64  `json:"point_id"`
	Platform   string  `json:"platform"`
	AssigneeID *uint64 `json:"assignee_id"`
	State      string  `json:"state"`
}

// Validate 校验需求内容和日期。
func (in Input) Validate() error {
	if strings.TrimSpace(in.Title) == "" || utf8.RuneCountInString(in.Title) > 255 || utf8.RuneCountInString(in.Goal) > 10000 || utf8.RuneCountInString(in.Scenario) > 10000 || utf8.RuneCountInString(in.URL) > 1024 {
		return ErrInvalid
	}
	if in.DueDate != "" {
		if _, err := time.Parse("2006-01-02", in.DueDate); err != nil {
			return ErrInvalid
		}
	}
	if in.URL != "" {
		u, err := url.Parse(in.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return ErrInvalid
		}
	}
	return nil
}

// Validate 校验接入任务的平台和状态。
func (in TaskInput) Validate() error {
	if in.PointID == 0 {
		return ErrInvalid
	}
	switch in.Platform {
	case "web", "ios", "android", "mini_program", "server":
	default:
		return ErrInvalid
	}
	switch in.State {
	case "PENDING", "DEVELOPING", "TESTING", "ONLINE":
	default:
		return ErrInvalid
	}
	if in.AssigneeID != nil && *in.AssigneeID == 0 {
		return ErrInvalid
	}
	return nil
}
func audit(ctx context.Context, tx *sql.Tx, projectID, actor, id uint64, action, resource string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_log (project_id,actor_id,action,resource_type,resource_id,after_snapshot) VALUES (?,?,?,?,?,?)`, projectID, actor, action, resource, id, string(b))
	return err
}

// Create 新建需求并在同一事务记录操作。
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
	res, err := tx.ExecContext(ctx, `INSERT INTO tracking_requirement (project_id,title,goal,scenario,requirement_url,due_date,created_by) VALUES (?,?,?,?,?,NULLIF(?,''),?)`, projectID, strings.TrimSpace(in.Title), in.Goal, in.Scenario, in.URL, in.DueDate, actor)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err = audit(ctx, tx, projectID, actor, uint64(id), "CREATE", "requirement", in); err != nil {
		return 0, err
	}
	return uint64(id), tx.Commit()
}

// Update 更新项目内的需求，并原子记录变更。
func (s *Service) Update(ctx context.Context, actor, projectID, requirementID uint64, in Input) error {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin); err != nil {
		return err
	}
	if err := in.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing uint64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tracking_requirement WHERE id=? AND project_id=? FOR UPDATE`, requirementID, projectID).Scan(&existing); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE tracking_requirement SET title=?,goal=?,scenario=?,requirement_url=?,due_date=NULLIF(?,'') WHERE id=? AND project_id=?`, strings.TrimSpace(in.Title), in.Goal, in.Scenario, in.URL, in.DueDate, requirementID, projectID)
	if err != nil {
		return err
	}
	if err = audit(ctx, tx, projectID, actor, requirementID, "UPDATE", "requirement", in); err != nil {
		return err
	}
	return tx.Commit()
}

// List 返回项目需求列表。
func (s *Service) List(ctx context.Context, actor, projectID uint64, before ...uint64) ([]Requirement, error) {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin, project.Viewer); err != nil {
		return nil, err
	}
	var cursor uint64
	if len(before) > 0 {
		cursor = before[0]
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,title,goal,scenario,requirement_url,COALESCE(DATE_FORMAT(due_date,'%Y-%m-%d'),''),created_by,created_at FROM tracking_requirement WHERE project_id=? AND (?=0 OR id<?) ORDER BY id DESC LIMIT 100`, projectID, cursor, cursor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Requirement{}
	for rows.Next() {
		var r Requirement
		if err = rows.Scan(&r.ID, &r.ProjectID, &r.Title, &r.Goal, &r.Scenario, &r.URL, &r.DueDate, &r.CreatedBy, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Tasks 返回需求下的接入任务，并限制项目范围。
func (s *Service) Tasks(ctx context.Context, actor, projectID, requirementID uint64) ([]Task, error) {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin, project.Viewer); err != nil {
		return nil, err
	}
	var exists uint64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM tracking_requirement WHERE id=? AND project_id=?`, requirementID, projectID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,t.requirement_id,t.point_id,p.name,t.platform,t.assignee_id,t.state FROM tracking_requirement_task t JOIN tracking_point p ON p.id=t.point_id WHERE t.requirement_id=? ORDER BY t.id`, requirementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		var t Task
		if err = rows.Scan(&t.ID, &t.RequirementID, &t.PointID, &t.PointName, &t.Platform, &t.AssigneeID, &t.State); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SaveTask 创建或更新项目内需求的按平台接入任务。
func (s *Service) SaveTask(ctx context.Context, actor, projectID, requirementID, taskID uint64, in TaskInput) error {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin); err != nil {
		return err
	}
	if err := in.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id uint64
	err = tx.QueryRowContext(ctx, `SELECT r.id FROM tracking_requirement r JOIN tracking_point p ON p.project_id=r.project_id WHERE r.id=? AND r.project_id=? AND p.id=? AND JSON_CONTAINS(p.supported_platforms,JSON_QUOTE(?)) FOR UPDATE`, requirementID, projectID, in.PointID, in.Platform).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalid
	}
	if err != nil {
		return err
	}
	if in.AssigneeID != nil {
		err = tx.QueryRowContext(ctx, `SELECT user_id FROM project_member WHERE project_id=? AND user_id=? AND is_active=TRUE`, projectID, *in.AssigneeID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalid
		}
		if err != nil {
			return err
		}
	}
	if taskID == 0 {
		res, e := tx.ExecContext(ctx, `INSERT INTO tracking_requirement_task (requirement_id,point_id,platform,assignee_id,state) VALUES (?,?,?,?,?)`, requirementID, in.PointID, in.Platform, in.AssigneeID, in.State)
		if e != nil {
			return e
		}
		n, e := res.LastInsertId()
		if e != nil {
			return e
		}
		taskID = uint64(n)
	} else {
		var existing uint64
		var pointID uint64
		var platform string
		err = tx.QueryRowContext(ctx, `SELECT id,point_id,platform FROM tracking_requirement_task WHERE id=? AND requirement_id=? FOR UPDATE`, taskID, requirementID).Scan(&existing, &pointID, &platform)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if pointID != in.PointID || platform != in.Platform {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE tracking_requirement_task SET point_id=?,platform=?,assignee_id=?,state=? WHERE id=?`, in.PointID, in.Platform, in.AssigneeID, in.State, taskID)
		if err != nil {
			return err
		}
	}
	if err = audit(ctx, tx, projectID, actor, taskID, "SAVE", "requirement_task", in); err != nil {
		return err
	}
	return tx.Commit()
}

// Acceptance 记录一次人工验收，不能代表生产数据已经入库。
type Acceptance struct {
	ID        uint64    `json:"id"`
	TaskID    uint64    `json:"task_id"`
	Result    string    `json:"result"`
	Note      string    `json:"note"`
	ActorID   uint64    `json:"actor_id"`
	CreatedAt time.Time `json:"created_at"`
}
type AcceptanceInput struct {
	Result string `json:"result"`
	Note   string `json:"note"`
}

// AddAcceptance 为指定项目的任务追加人工验收记录。
func (s *Service) AddAcceptance(ctx context.Context, actor, projectID, requirementID, taskID uint64, in AcceptanceInput) error {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin); err != nil {
		return err
	}
	if (in.Result != "PASS" && in.Result != "FAIL") || len([]rune(in.Note)) > 2000 {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id uint64
	err = tx.QueryRowContext(ctx, `SELECT t.id FROM tracking_requirement_task t JOIN tracking_requirement r ON r.id=t.requirement_id WHERE t.id=? AND r.id=? AND r.project_id=? FOR UPDATE`, taskID, requirementID, projectID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO tracking_task_acceptance (task_id,result,note,actor_id) VALUES (?,?,?,?)`, taskID, in.Result, in.Note, actor)
	if err != nil {
		return err
	}
	n, err := result.LastInsertId()
	if err != nil {
		return err
	}
	if err = audit(ctx, tx, projectID, actor, uint64(n), "CREATE", "task_acceptance", in); err != nil {
		return err
	}
	return tx.Commit()
}

// ListAcceptances 返回指定任务的验收历史。
func (s *Service) ListAcceptances(ctx context.Context, actor, projectID, requirementID, taskID uint64) ([]Acceptance, error) {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin, project.Viewer); err != nil {
		return nil, err
	}
	var id uint64
	err := s.db.QueryRowContext(ctx, `SELECT t.id FROM tracking_requirement_task t JOIN tracking_requirement r ON r.id=t.requirement_id WHERE t.id=? AND r.id=? AND r.project_id=?`, taskID, requirementID, projectID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,task_id,result,note,actor_id,created_at FROM tracking_task_acceptance WHERE task_id=? ORDER BY id DESC LIMIT 100`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Acceptance{}
	for rows.Next() {
		var a Acceptance
		if err = rows.Scan(&a.ID, &a.TaskID, &a.Result, &a.Note, &a.ActorID, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteTask 删除尚无验收记录的任务并保留审计信息。
func (s *Service) DeleteTask(ctx context.Context, actor, projectID, requirementID, taskID uint64) error {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var old Task
	err = tx.QueryRowContext(ctx, `SELECT t.id,t.requirement_id,t.point_id,t.platform,t.assignee_id,t.state FROM tracking_requirement_task t JOIN tracking_requirement r ON r.id=t.requirement_id WHERE t.id=? AND r.id=? AND r.project_id=? FOR UPDATE`, taskID, requirementID, projectID).Scan(&old.ID, &old.RequirementID, &old.PointID, &old.Platform, &old.AssigneeID, &old.State)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracking_task_acceptance WHERE task_id=?`, taskID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrConflict
	}
	if err = audit(ctx, tx, projectID, actor, taskID, "DELETE", "requirement_task", old); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tracking_requirement_task WHERE id=?`, taskID); err != nil {
		return err
	}
	return tx.Commit()
}
