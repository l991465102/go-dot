package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/tracking"
)

var _ ports.TrackingRepository = (*TrackingRepository)(nil)

type TrackingRepository struct{ db *sql.DB }

func NewTrackingRepository(db *sql.DB) *TrackingRepository { return &TrackingRepository{db: db} }

func (r *TrackingRepository) CreateCategory(ctx context.Context, c tracking.Category) (tracking.Category, error) {
	result, err := r.db.ExecContext(ctx, `INSERT INTO tracking_category(project_id,parent_id,code,name,level,description,sort_order,is_active,created_by) VALUES(?,?,?,?,?,?,?,?,?)`, c.ProjectID, c.ParentID, c.Code, c.Name, c.Level, nullable(c.Description), c.SortOrder, c.Active, c.CreatedBy)
	if err != nil {
		return tracking.Category{}, fmt.Errorf("insert tracking category: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return tracking.Category{}, err
	}
	c.ID = uint64(id)
	return c, nil
}

func (r *TrackingRepository) FindCategory(ctx context.Context, id uint64) (tracking.Category, error) {
	var c tracking.Category
	var parent sql.NullInt64
	var description sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT id,project_id,parent_id,code,name,level,description,sort_order,is_active,created_by FROM tracking_category WHERE id=?`, id).Scan(&c.ID, &c.ProjectID, &parent, &c.Code, &c.Name, &c.Level, &description, &c.SortOrder, &c.Active, &c.CreatedBy)
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
		return created, fmt.Errorf("insert tracking point: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return created, err
	}
	p.ID = uint64(id)
	if err = insertProperties(ctx, tx, p.ID, p.Properties); err != nil {
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

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func findPoint(ctx context.Context, q queryer, id uint64, lock bool) (tracking.Point, error) {
	query := pointSelectSQL + ` WHERE p.id=?`
	if lock {
		query += ` FOR UPDATE`
	}
	var p tracking.Point
	var desc, triggerPage, triggerTiming, triggerCount, abnormal, productOwner, developerOwner, requirement, depreciation sql.NullString
	var replacement sql.NullInt64
	var platforms, channels []byte
	var published sql.NullInt64
	err := q.QueryRowContext(ctx, query, id).Scan(&p.ID, &p.ProjectID, &p.CategoryID, &p.Code, &p.Name, &desc, &p.BehaviorType, &p.State, &p.DevelopmentState, &triggerPage, &triggerTiming, &triggerCount, &abnormal, &p.CountPV, &p.CountUV, &p.UVMode, &p.TimeBasis, &platforms, &channels, &productOwner, &developerOwner, &requirement, &published, &depreciation, &replacement, &p.CreatedBy, &p.UpdatedBy, &p.Category.ID, &p.Category.ProjectID, &p.Category.Level, &p.Category.Active)
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

const pointSelectSQL = `SELECT p.id,p.project_id,p.category_id,p.code,p.name,p.description,p.behavior_type,p.definition_state,p.development_state,p.trigger_page,p.trigger_timing,p.trigger_count_rule,p.abnormal_trigger_rule,p.count_pv,p.count_uv,p.uv_mode,p.time_basis,p.supported_platforms,p.supported_channels,p.product_owner,p.developer_owner,p.requirement_url,p.published_version,p.deprecation_reason,p.replacement_point_id,p.created_by,p.updated_by,c.id,c.project_id,c.level,c.is_active FROM tracking_point p JOIN tracking_category c ON c.id=p.category_id`

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
	platforms, _ := json.Marshal(nonNilStrings(p.SupportedPlatforms))
	channels, _ := json.Marshal(nonNilStrings(p.SupportedChannels))
	result, err := tx.ExecContext(ctx, `UPDATE tracking_point SET category_id=?,code=?,name=?,description=?,behavior_type=?,development_state=?,trigger_page=?,trigger_timing=?,trigger_count_rule=?,abnormal_trigger_rule=?,count_pv=?,count_uv=?,uv_mode=?,time_basis=?,supported_platforms=?,supported_channels=?,product_owner=?,developer_owner=?,requirement_url=?,updated_by=? WHERE id=? AND definition_state='DRAFT'`, p.CategoryID, p.Code, p.Name, nullable(p.Description), p.BehaviorType, p.DevelopmentState, nullable(p.TriggerPage), nullable(p.TriggerTiming), nullable(p.TriggerCountRule), nullable(p.AbnormalTriggerRule), p.CountPV, p.CountUV, p.UVMode, p.TimeBasis, platforms, channels, nullable(p.ProductOwner), nullable(p.DeveloperOwner), nullable(p.RequirementURL), p.UpdatedBy, p.ID)
	if err != nil {
		return updated, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return updated, tracking.ErrImmutable
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tracking_point_property WHERE point_id=?`, p.ID); err != nil {
		return updated, err
	}
	if err = insertProperties(ctx, tx, p.ID, p.Properties); err != nil {
		return updated, err
	}
	if err = tx.Commit(); err != nil {
		return updated, err
	}
	return p, nil
}

func (r *TrackingRepository) DeleteDraft(ctx context.Context, id uint64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM tracking_point WHERE id=? AND definition_state='DRAFT'`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return tracking.ErrImmutable
	}
	return nil
}

func (r *TrackingRepository) Publish(ctx context.Context, id, actorID uint64) (snapshot tracking.Snapshot, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return snapshot, err
	}
	defer rollbackOnError(tx, &err)
	p, err := findPoint(ctx, tx, id, true)
	if err != nil {
		return snapshot, err
	}
	if p.State != tracking.Draft {
		return snapshot, tracking.ErrImmutable
	}
	if err = p.ValidateForPublication(); err != nil {
		return snapshot, err
	}
	version := p.PublishedVersion + 1
	snapshot = p.Snapshot(actorID, version)
	canonical, err := json.Marshal(snapshot)
	if err != nil {
		return snapshot, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO tracking_point_snapshot(project_id,point_id,version,point_code,schema_snapshot,published_by) VALUES(?,?,?,?,?,?)`, p.ProjectID, p.ID, version, p.Code, canonical, actorID)
	if err != nil {
		return snapshot, err
	}
	snapshotID, err := result.LastInsertId()
	if err != nil {
		return snapshot, err
	}
	snapshot.ID = uint64(snapshotID)
	result, err = tx.ExecContext(ctx, `UPDATE tracking_point SET definition_state='PUBLISHED',published_version=?,published_at=CURRENT_TIMESTAMP(6),updated_by=? WHERE id=? AND definition_state='DRAFT'`, version, actorID, id)
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
	snapshot.PublishedAt = time.Now().UTC()
	return snapshot, nil
}

func (r *TrackingRepository) Deprecate(ctx context.Context, id, actorID uint64, reason string, replacementID *uint64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE tracking_point SET definition_state='DEPRECATED',deprecated_at=CURRENT_TIMESTAMP(6),deprecation_reason=?,replacement_point_id=?,updated_by=? WHERE id=? AND definition_state='PUBLISHED'`, reason, replacementID, actorID, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return tracking.ErrPointNotPublished
	}
	return nil
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
