package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/tracking"
)

var _ ports.TrackingRepository = (*TrackingRepository)(nil)

type TrackingRepository struct{ db *sql.DB }

func NewTrackingRepository(db *sql.DB) *TrackingRepository { return &TrackingRepository{db: db} }

func (r *TrackingRepository) CreateCategory(ctx context.Context, c tracking.Category) (created tracking.Category, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return created, err
	}
	defer rollbackOnError(tx, &err)
	result, err := tx.ExecContext(ctx, `INSERT INTO tracking_category(project_id,parent_id,code,name,level,description,sort_order,is_active,created_by) VALUES(?,?,?,?,?,?,?,?,?)`, c.ProjectID, c.ParentID, c.Code, c.Name, c.Level, nullable(c.Description), c.SortOrder, c.Active, c.CreatedBy)
	if err != nil {
		return created, translateConflict(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return created, err
	}
	c.ID = uint64(id)
	after, _ := json.Marshal(c)
	if err = insertAudit(ctx, tx, c.ProjectID, c.CreatedBy, "CREATE_CATEGORY", "TRACKING_CATEGORY", c.ID, nil, after); err != nil {
		return created, err
	}
	if err = tx.Commit(); err != nil {
		return created, err
	}
	return c, nil
}

func (r *TrackingRepository) FindCategory(ctx context.Context, id uint64) (tracking.Category, error) {
	return findCategory(ctx, r.db, 0, id)
}
func (r *TrackingRepository) FindCategoryInProject(ctx context.Context, projectID, id uint64) (tracking.Category, error) {
	return findCategory(ctx, r.db, projectID, id)
}
func findCategory(ctx context.Context, q queryer, projectID, id uint64) (tracking.Category, error) {
	var c tracking.Category
	var parent sql.NullInt64
	var description sql.NullString
	query := `SELECT id,project_id,parent_id,code,name,level,description,sort_order,is_active,created_by FROM tracking_category WHERE id=?`
	args := []any{id}
	if projectID != 0 {
		query += ` AND project_id=?`
		args = append(args, projectID)
	}
	err := q.QueryRowContext(ctx, query, args...).Scan(&c.ID, &c.ProjectID, &parent, &c.Code, &c.Name, &c.Level, &description, &c.SortOrder, &c.Active, &c.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return c, tracking.ErrCategoryNotFound
	}
	if err != nil {
		return c, err
	}
	if parent.Valid {
		v := uint64(parent.Int64)
		c.ParentID = &v
	}
	c.Description = description.String
	return c, nil
}

func (r *TrackingRepository) CreateDraft(ctx context.Context, p tracking.Point) (created tracking.Point, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return created, err
	}
	defer rollbackOnError(tx, &err)
	platforms, _ := json.Marshal(nonNilStrings(p.SupportedPlatforms))
	channels, _ := json.Marshal(nonNilStrings(p.SupportedChannels))
	result, err := tx.ExecContext(ctx, insertPointSQL, p.ProjectID, p.CategoryID, p.Code, p.Name, nullable(p.Description), p.BehaviorType, tracking.Draft, p.DevelopmentState, nullable(p.TriggerPage), nullable(p.TriggerTiming), nullable(p.TriggerCountRule), nullable(p.AbnormalTriggerRule), p.CountPV, p.CountUV, p.UVMode, p.TimeBasis, platforms, channels, nullable(p.ProductOwner), nullable(p.DeveloperOwner), nullable(p.RequirementURL), p.CreatedBy, p.UpdatedBy)
	if err != nil {
		return created, translateConflict(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return created, err
	}
	p.ID = uint64(id)
	if err = insertProperties(ctx, tx, p.ID, p.Properties); err != nil {
		return created, err
	}
	after, _ := json.Marshal(p)
	action := p.AuditAction
	if action == "" {
		action = "CREATE_DRAFT"
	}
	if err = insertAudit(ctx, tx, p.ProjectID, p.UpdatedBy, action, "TRACKING_POINT", p.ID, nil, after); err != nil {
		return created, err
	}
	if err = tx.Commit(); err != nil {
		return created, err
	}
	return p, nil
}

const insertPointSQL = `INSERT INTO tracking_point(project_id,category_id,code,name,description,behavior_type,definition_state,development_state,trigger_page,trigger_timing,trigger_count_rule,abnormal_trigger_rule,count_pv,count_uv,uv_mode,time_basis,supported_platforms,supported_channels,product_owner,developer_owner,requirement_url,created_by,updated_by) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

func (r *TrackingRepository) FindPoint(ctx context.Context, id uint64) (tracking.Point, error) {
	return findPoint(ctx, r.db, id, false)
}
func (r *TrackingRepository) FindPointInProject(ctx context.Context, projectID, id uint64) (tracking.Point, error) {
	return findPoint(ctx, r.db, id, false, projectID)
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func findPoint(ctx context.Context, q queryer, id uint64, lock bool, projectIDs ...uint64) (tracking.Point, error) {
	query := pointSelectSQL + ` WHERE p.id=?`
	args := []any{id}
	if len(projectIDs) > 0 {
		query += ` AND p.project_id=?`
		args = append(args, projectIDs[0])
	}
	if lock {
		query += ` FOR UPDATE`
	}
	var p tracking.Point
	var desc, triggerPage, triggerTiming, triggerCount, abnormal, productOwner, developerOwner, requirement, depreciation sql.NullString
	var replacement sql.NullInt64
	var platforms, channels []byte
	var published sql.NullInt64
	var parent sql.NullInt64
	err := q.QueryRowContext(ctx, query, args...).Scan(&p.ID, &p.ProjectID, &p.CategoryID, &p.Code, &p.Name, &desc, &p.BehaviorType, &p.State, &p.DevelopmentState, &triggerPage, &triggerTiming, &triggerCount, &abnormal, &p.CountPV, &p.CountUV, &p.UVMode, &p.TimeBasis, &platforms, &channels, &productOwner, &developerOwner, &requirement, &published, &depreciation, &replacement, &p.CreatedBy, &p.UpdatedBy, &p.Category.ID, &p.Category.ProjectID, &p.Category.Level, &p.Category.Active, &p.Category.Code, &p.Category.Name, &parent)
	if errors.Is(err, sql.ErrNoRows) {
		return p, tracking.ErrPointNotFound
	}
	if err != nil {
		return p, err
	}
	p.Description = desc.String
	p.TriggerPage = triggerPage.String
	p.TriggerTiming = triggerTiming.String
	p.TriggerCountRule = triggerCount.String
	p.AbnormalTriggerRule = abnormal.String
	p.ProductOwner = productOwner.String
	p.DeveloperOwner = developerOwner.String
	p.RequirementURL = requirement.String
	p.DeprecationReason = depreciation.String
	if published.Valid {
		p.PublishedVersion = uint(published.Int64)
	}
	if replacement.Valid {
		v := uint64(replacement.Int64)
		p.ReplacementPointID = &v
	}
	if parent.Valid {
		v := uint64(parent.Int64)
		p.Category.ParentID = &v
	}
	if err := json.Unmarshal(platforms, &p.SupportedPlatforms); err != nil {
		return p, err
	}
	if err := json.Unmarshal(channels, &p.SupportedChannels); err != nil {
		return p, err
	}
	properties, err := findProperties(ctx, q, id)
	if err != nil {
		return p, err
	}
	p.Properties = properties
	return p, nil
}

const pointSelectSQL = `SELECT p.id,p.project_id,p.category_id,p.code,p.name,p.description,p.behavior_type,p.definition_state,p.development_state,p.trigger_page,p.trigger_timing,p.trigger_count_rule,p.abnormal_trigger_rule,p.count_pv,p.count_uv,p.uv_mode,p.time_basis,p.supported_platforms,p.supported_channels,p.product_owner,p.developer_owner,p.requirement_url,p.published_version,p.deprecation_reason,p.replacement_point_id,p.created_by,p.updated_by,c.id,c.project_id,c.level,c.is_active,c.code,c.name,c.parent_id FROM tracking_point p JOIN tracking_category c ON c.id=p.category_id`

func findProperties(ctx context.Context, q queryer, pointID uint64) ([]tracking.Property, error) {
	rows, err := q.QueryContext(ctx, `SELECT code,name,data_type,is_required,enum_values,array_item_type,example_value,description,sort_order FROM tracking_point_property WHERE point_id=? ORDER BY sort_order,id`, pointID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	properties := []tracking.Property{}
	for rows.Next() {
		var p tracking.Property
		var enums, example []byte
		var item, description sql.NullString
		if err := rows.Scan(&p.Code, &p.Name, &p.Type, &p.Required, &enums, &item, &example, &description, &p.SortOrder); err != nil {
			return nil, err
		}
		p.ArrayItemType = tracking.PropertyType(item.String)
		p.Description = description.String
		if len(enums) > 0 {
			if err := json.Unmarshal(enums, &p.EnumValues); err != nil {
				return nil, err
			}
		}
		if len(example) > 0 {
			if err := json.Unmarshal(example, &p.ExampleValue); err != nil {
				return nil, err
			}
		}
		properties = append(properties, p)
	}
	return properties, rows.Err()
}

func (r *TrackingRepository) UpdateDraft(ctx context.Context, p tracking.Point) (updated tracking.Point, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return updated, err
	}
	defer rollbackOnError(tx, &err)
	beforePoint, err := findPoint(ctx, tx, p.ID, true, p.ProjectID)
	if err != nil {
		return updated, err
	}
	if beforePoint.State != tracking.Draft {
		return updated, tracking.ErrImmutable
	}
	platforms, _ := json.Marshal(nonNilStrings(p.SupportedPlatforms))
	channels, _ := json.Marshal(nonNilStrings(p.SupportedChannels))
	result, err := tx.ExecContext(ctx, `UPDATE tracking_point SET category_id=?,code=?,name=?,description=?,behavior_type=?,development_state=?,trigger_page=?,trigger_timing=?,trigger_count_rule=?,abnormal_trigger_rule=?,count_pv=?,count_uv=?,uv_mode=?,time_basis=?,supported_platforms=?,supported_channels=?,product_owner=?,developer_owner=?,requirement_url=?,updated_by=? WHERE project_id=? AND id=? AND definition_state='DRAFT'`, p.CategoryID, p.Code, p.Name, nullable(p.Description), p.BehaviorType, p.DevelopmentState, nullable(p.TriggerPage), nullable(p.TriggerTiming), nullable(p.TriggerCountRule), nullable(p.AbnormalTriggerRule), p.CountPV, p.CountUV, p.UVMode, p.TimeBasis, platforms, channels, nullable(p.ProductOwner), nullable(p.DeveloperOwner), nullable(p.RequirementURL), p.UpdatedBy, p.ProjectID, p.ID)
	if err != nil {
		return updated, translateConflict(err)
	}
	_ = result
	if _, err = tx.ExecContext(ctx, `DELETE FROM tracking_point_property WHERE point_id=?`, p.ID); err != nil {
		return updated, err
	}
	if err = insertProperties(ctx, tx, p.ID, p.Properties); err != nil {
		return updated, err
	}
	before, _ := json.Marshal(beforePoint)
	after, _ := json.Marshal(p)
	if err = insertAudit(ctx, tx, p.ProjectID, p.UpdatedBy, "UPDATE_DRAFT", "TRACKING_POINT", p.ID, before, after); err != nil {
		return updated, err
	}
	if err = tx.Commit(); err != nil {
		return updated, err
	}
	return p, nil
}

func (r *TrackingRepository) UpdateDevelopmentState(ctx context.Context, projectID, id, actorID uint64, state tracking.DevelopmentState) (updated tracking.Point, err error) {
	tx, e := r.db.BeginTx(ctx, nil)
	if e != nil {
		return updated, e
	}
	defer rollbackOnError(tx, &err)
	p, e := findPoint(ctx, tx, id, true, projectID)
	if e != nil {
		return updated, e
	}
	if p.State != tracking.Draft && p.State != tracking.Published {
		return updated, tracking.NewCodedError("POINT_STATE_INVALID", "development state can only change for draft or published points")
	}
	if p.DevelopmentState == state {
		if err = tx.Commit(); err != nil {
			return updated, err
		}
		return p, nil
	}
	before, _ := json.Marshal(p)
	result, e := tx.ExecContext(ctx, `UPDATE tracking_point SET development_state=?,updated_by=? WHERE project_id=? AND id=? AND definition_state IN ('DRAFT','PUBLISHED')`, state, actorID, projectID, id)
	if e != nil {
		return updated, e
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return updated, tracking.ErrPointNotFound
	}
	p.DevelopmentState = state
	p.UpdatedBy = actorID
	after, _ := json.Marshal(p)
	if err = insertAudit(ctx, tx, projectID, actorID, "UPDATE_DEVELOPMENT_STATE", "TRACKING_POINT", id, before, after); err != nil {
		return updated, err
	}
	if err = tx.Commit(); err != nil {
		return updated, err
	}
	return p, nil
}

func (r *TrackingRepository) DeleteDraft(ctx context.Context, projectID, id, actorID uint64) (err error) {
	tx, e := r.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer rollbackOnError(tx, &err)
	p, e := findPoint(ctx, tx, id, true, projectID)
	if e != nil {
		return e
	}
	if p.State != tracking.Draft {
		return tracking.ErrImmutable
	}
	before, _ := json.Marshal(p)
	if err = insertAudit(ctx, tx, projectID, actorID, "DELETE_DRAFT", "TRACKING_POINT", id, before, nil); err != nil {
		return err
	}
	result, e := tx.ExecContext(ctx, `DELETE FROM tracking_point WHERE project_id=? AND id=? AND definition_state='DRAFT'`, projectID, id)
	if e != nil {
		return e
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return tracking.ErrImmutable
	}
	return tx.Commit()
}

func (r *TrackingRepository) Publish(ctx context.Context, projectID, id, actorID uint64) (snapshot tracking.Snapshot, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return snapshot, err
	}
	defer rollbackOnError(tx, &err)
	p, err := findPoint(ctx, tx, id, true, projectID)
	if err != nil {
		return snapshot, err
	}
	if p.State != tracking.Draft {
		return snapshot, tracking.ErrImmutable
	}
	if err = p.ValidateForPublication(); err != nil {
		return snapshot, err
	}
	path, err := loadCategoryPath(ctx, tx, p.Category)
	if err != nil {
		return snapshot, err
	}
	p.CategoryPath = path
	version := p.PublishedVersion + 1
	publishedAt := time.Now().UTC()
	snapshot = p.Snapshot(actorID, version, publishedAt)
	canonical, err := json.Marshal(snapshot)
	if err != nil {
		return snapshot, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tracking_point_snapshot(project_id,point_id,version,point_code,schema_snapshot,published_by,published_at) VALUES(?,?,?,?,?,?,?)`, p.ProjectID, p.ID, version, p.Code, canonical, actorID, publishedAt)
	if err != nil {
		return snapshot, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE tracking_point SET definition_state='PUBLISHED',published_version=?,published_at=?,updated_by=? WHERE project_id=? AND id=? AND definition_state='DRAFT'`, version, publishedAt, actorID, projectID, id)
	if err != nil {
		return snapshot, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return snapshot, tracking.ErrImmutable
	}
	before, _ := json.Marshal(p)
	p.State = tracking.Published
	p.PublishedVersion = version
	after, _ := json.Marshal(p)
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_log(project_id,actor_id,action,resource_type,resource_id,before_snapshot,after_snapshot) VALUES(?,?,'PUBLISH','TRACKING_POINT',?,?,?)`, p.ProjectID, actorID, p.ID, before, after); err != nil {
		return snapshot, err
	}
	if err = tx.Commit(); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func (r *TrackingRepository) Deprecate(ctx context.Context, projectID, id, actorID uint64, reason string, replacementID *uint64) (err error) {
	if replacementID != nil && *replacementID == id {
		return tracking.NewCodedError("REPLACEMENT_INVALID", "point cannot replace itself")
	}
	tx, e := r.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer rollbackOnError(tx, &err)
	p, e := findPoint(ctx, tx, id, true, projectID)
	if e != nil {
		return e
	}
	if p.State != tracking.Published {
		return tracking.ErrPointNotPublished
	}
	if replacementID != nil {
		replacement, e := findPoint(ctx, tx, *replacementID, true, projectID)
		if e != nil {
			return e
		}
		if replacement.State != tracking.Published {
			return tracking.NewCodedError("REPLACEMENT_INVALID", "replacement must be published")
		}
	}
	before, _ := json.Marshal(p)
	result, e := tx.ExecContext(ctx, `UPDATE tracking_point SET definition_state='DEPRECATED',deprecated_at=CURRENT_TIMESTAMP(6),deprecation_reason=?,replacement_point_id=?,updated_by=? WHERE project_id=? AND id=? AND definition_state='PUBLISHED'`, reason, replacementID, actorID, projectID, id)
	if e != nil {
		return e
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return tracking.ErrPointNotPublished
	}
	p.State = tracking.Deprecated
	p.DeprecationReason = reason
	p.ReplacementPointID = replacementID
	after, _ := json.Marshal(p)
	if err = insertAudit(ctx, tx, projectID, actorID, "DEPRECATE", "TRACKING_POINT", id, before, after); err != nil {
		return err
	}
	return tx.Commit()
}

func insertProperties(ctx context.Context, tx *sql.Tx, pointID uint64, properties []tracking.Property) error {
	for _, p := range properties {
		var enums, example any
		if len(p.EnumValues) > 0 {
			b, err := json.Marshal(p.EnumValues)
			if err != nil {
				return err
			}
			enums = b
		}
		if p.ExampleValue != nil {
			b, err := json.Marshal(p.ExampleValue)
			if err != nil {
				return err
			}
			example = b
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tracking_point_property(point_id,code,name,data_type,is_required,enum_values,array_item_type,example_value,description,sort_order) VALUES(?,?,?,?,?,?,?,?,?,?)`, pointID, p.Code, p.Name, p.Type, p.Required, enums, nullable(string(p.ArrayItemType)), example, nullable(p.Description), p.SortOrder); err != nil {
			return err
		}
	}
	return nil
}
func rollbackOnError(tx *sql.Tx, err *error) {
	if *err != nil {
		_ = tx.Rollback()
	}
}
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func insertAudit(ctx context.Context, tx *sql.Tx, projectID, actorID uint64, action, resource string, resourceID uint64, before, after []byte) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_log(project_id,actor_id,action,resource_type,resource_id,before_snapshot,after_snapshot) VALUES(?,?,?,?,?,?,?)`, projectID, actorID, action, resource, resourceID, nullableJSON(before), nullableJSON(after))
	return err
}
func nullableJSON(v []byte) any {
	if len(v) == 0 {
		return nil
	}
	return v
}
func translateConflict(err error) error {
	var me *mysqldriver.MySQLError
	if errors.As(err, &me) && me.Number == 1062 {
		return tracking.ErrConflict
	}
	return err
}
func loadCategoryPath(ctx context.Context, q queryer, leaf tracking.Category) ([]tracking.CategoryRef, error) {
	path := []tracking.CategoryRef{{ID: leaf.ID, Code: leaf.Code, Name: leaf.Name, Level: leaf.Level}}
	parent := leaf.ParentID
	for parent != nil {
		var c tracking.Category
		var next sql.NullInt64
		if err := q.QueryRowContext(ctx, `SELECT id,project_id,code,name,level,parent_id,is_active FROM tracking_category WHERE id=? FOR UPDATE`, *parent).Scan(&c.ID, &c.ProjectID, &c.Code, &c.Name, &c.Level, &next, &c.Active); err != nil {
			return nil, err
		}
		if c.ProjectID != leaf.ProjectID {
			return nil, tracking.NewCodedError("CATEGORY_INVALID", "category path crosses projects")
		}
		path = append(path, tracking.CategoryRef{ID: c.ID, Code: c.Code, Name: c.Name, Level: c.Level})
		if next.Valid {
			v := uint64(next.Int64)
			parent = &v
		} else {
			parent = nil
		}
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	if len(path) != 3 || path[0].Level != tracking.Domain || path[1].Level != tracking.Module || path[2].Level != tracking.Scene {
		return nil, tracking.NewCodedError("CATEGORY_INVALID", "complete DOMAIN/MODULE/SCENE path required")
	}
	return path, nil
}
