package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lzw7758/go-dot/internal/auth"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/tracking"
)

func TestProjectRepositoryRejectsRotationDuringGraceUnderRowLock(t *testing.T) {
	now := time.Now().UTC()
	script, db := openScriptedDB(t, []driver.Value{now.Add(time.Hour)})
	repo := NewProjectRepository(db)

	err := repo.RotateWriteKey(context.Background(), 9, "new-hash", now, now.Add(24*time.Hour))
	require.ErrorIs(t, err, project.ErrWriteKeyRotationInProgress)
	require.Len(t, script.queries, 1)
	require.Contains(t, strings.ToUpper(script.queries[0]), "FOR UPDATE")
	require.Empty(t, script.execQueries)
	require.Equal(t, 1, script.rollbacks)
}

func TestProjectRepositoryFindsCurrentOrUnexpiredPreviousWriteKey(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	row := []driver.Value{uint64(9), "mall", "Mall", "current", "previous", now.Add(time.Hour), []byte(`["https://app.example"]`), "Asia/Shanghai", []byte("salt"), true, uint64(1)}
	script, db := openScriptedDB(t, row)
	p, err := NewProjectRepository(db).FindProjectByWriteKeyHash(context.Background(), "previous", now)
	require.NoError(t, err)
	require.Equal(t, uint64(9), p.ID)
	require.Equal(t, []string{"https://app.example"}, p.AllowedOrigins)
	require.Contains(t, script.queries[0], "previous_write_key_expires_at>?")
}

func TestProjectRepositoryRejectsUnknownOrExpiredWriteKey(t *testing.T) {
	_, db := openScriptedDB(t, nil)
	_, err := NewProjectRepository(db).FindProjectByWriteKeyHash(context.Background(), "expired", time.Now())
	require.ErrorIs(t, err, project.ErrProjectNotFound)
}

func TestProjectRepositoryExpiresPreviousWriteKeysTransactionally(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.execResults = []driver.Result{scriptResult{affected: 2}}
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	affected, err := NewProjectRepository(db).ExpirePreviousWriteKeys(context.Background(), now)
	require.NoError(t, err)
	require.EqualValues(t, 2, affected)
	require.Len(t, script.execQueries, 1)
	require.Contains(t, script.execQueries[0], "previous_write_key_hash=NULL")
	require.Equal(t, 1, script.commits)
}

func TestProjectSettingsUpdateAndAuditCommitTogether(t *testing.T) {
	row := []driver.Value{uint64(9), "mall", "Mall", []byte(`["https://old.example"]`), "Asia/Shanghai", true, uint64(1)}
	script, db := openScriptedDB(t, row)
	script.execResults = []driver.Result{scriptResult{affected: 1}, scriptResult{affected: 1}}
	updated, err := NewProjectRepository(db).UpdateProjectSettings(context.Background(), 9, 1, project.SettingsInput{Timezone: "UTC", AllowedOrigins: []string{"https://new.example"}})
	require.NoError(t, err)
	require.Equal(t, "UTC", updated.Timezone)
	require.Len(t, script.execQueries, 2)
	require.Contains(t, script.execQueries[1], "audit_log")
	require.Equal(t, 1, script.commits)
}

func TestProjectSettingsAuditFailureRollsBackMutation(t *testing.T) {
	row := []driver.Value{uint64(9), "mall", "Mall", []byte(`[]`), "Asia/Shanghai", true, uint64(1)}
	script, db := openScriptedDB(t, row)
	script.execErrors = []error{nil, errors.New("audit unavailable")}
	_, err := NewProjectRepository(db).UpdateProjectSettings(context.Background(), 9, 1, project.SettingsInput{Timezone: "UTC"})
	require.ErrorContains(t, err, "audit unavailable")
	require.Zero(t, script.commits)
	require.Equal(t, 1, script.rollbacks)
}

func TestProjectMemberCannotDemoteLastAdmin(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{{uint64(2), "owner", "Owner"}, {"ADMIN", true}, {int64(1)}}
	_, err := NewProjectRepository(db).AddProjectMember(context.Background(), 9, 1, project.MemberInput{Username: "owner", Role: project.Viewer})
	require.ErrorIs(t, err, project.ErrInvalidProject)
	require.Empty(t, script.execQueries)
	require.Equal(t, 1, script.rollbacks)
}

func TestTrackingRepositoryLoadsOnlyPublishedImmutableSnapshot(t *testing.T) {
	snapshot := tracking.Snapshot{ProjectID: 9, PointID: 7, PointCode: "product_detail_view", Version: 1}
	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	script, db := openScriptedDB(t, []driver.Value{uint64(41), raw})
	loaded, err := NewTrackingRepository(db).FindPublishedSnapshot(context.Background(), 9, "product_detail_view")
	require.NoError(t, err)
	require.Equal(t, uint64(41), loaded.ID)
	require.Contains(t, script.queries[0], "definition_state='PUBLISHED'")
}

func TestTrackingRepositoryResolvesInactiveHistoricalCategoryInsideProjectAndLevel(t *testing.T) {
	script, db := openScriptedDB(t, []driver.Value{uint64(41)})
	domainID, moduleID, sceneID, err := NewTrackingRepository(db).ResolveCategoryIDs(context.Background(), 9, "mall", "", "")
	require.NoError(t, err)
	require.EqualValues(t, 41, domainID)
	require.Zero(t, moduleID)
	require.Zero(t, sceneID)
	require.Contains(t, script.queries[0], "project_id=? AND code=? AND level=?")
	require.NotContains(t, script.queries[0], "is_active=TRUE")
}

func TestTrackingRepositoryKeepsSeparateActiveCategoryResolverForGovernance(t *testing.T) {
	script, db := openScriptedDB(t, []driver.Value{uint64(41)})
	_, _, _, err := NewTrackingRepository(db).ResolveActiveCategoryIDs(context.Background(), 9, "mall", "", "")
	require.NoError(t, err)
	require.Contains(t, script.queries[0], "is_active=TRUE")
}

func TestTrackingRepositoryMissingCategoryCodeIsNotFound(t *testing.T) {
	_, db := openScriptedDB(t, nil)
	_, _, _, err := NewTrackingRepository(db).ResolveCategoryIDs(context.Background(), 9, "missing", "", "")
	require.ErrorIs(t, err, tracking.ErrCategoryNotFound)
}

func TestTrackingCreateCategoryLocksActiveParentBeforeInsert(t *testing.T) {
	parent := uint64(1)
	script, db := openScriptedDB(t, categoryRow(1, 9, nil, "mall", "Mall", "DOMAIN", true))
	script.execResults = []driver.Result{scriptResult{lastID: 2, affected: 1}, scriptResult{affected: 1}}
	_, err := NewTrackingRepository(db).CreateCategory(context.Background(), tracking.Category{ProjectID: 9, ParentID: &parent, Code: "product", Name: "Product", Level: tracking.Module, Active: true, CreatedBy: 1})
	require.NoError(t, err)
	require.NotEmpty(t, script.queries)
	require.Contains(t, strings.ToUpper(script.queries[0]), "FOR UPDATE")
	require.Equal(t, 1, script.commits)
}

func TestTrackingCategoryUnchangedUpdateIsIdempotentWithoutAudit(t *testing.T) {
	script, db := openScriptedDB(t, categoryRow(3, 9, uint64Ptr(2), "detail", "Detail", "SCENE", true))
	updated, err := NewTrackingRepository(db).UpdateCategory(context.Background(), 9, 3, 1, tracking.UpdateCategory{Name: "Detail", SortOrder: 0, Active: true})
	require.NoError(t, err)
	require.Equal(t, "Detail", updated.Name)
	require.Contains(t, strings.ToUpper(script.queries[len(script.queries)-1]), "FOR UPDATE")
	require.Empty(t, script.execQueries)
	require.Equal(t, 1, script.commits)
}

func TestTrackingCategoryEnableLocksAndRequiresActiveParent(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{categoryRow(3, 9, uint64Ptr(2), "detail", "Detail", "SCENE", false), categoryRow(2, 9, uint64Ptr(1), "product", "Product", "MODULE", false), categoryRow(3, 9, uint64Ptr(2), "detail", "Detail", "SCENE", false)}
	_, err := NewTrackingRepository(db).UpdateCategory(context.Background(), 9, 3, 1, tracking.UpdateCategory{Name: "Detail", Active: true})
	require.ErrorContains(t, err, "CATEGORY_INVALID")
	require.Empty(t, script.execQueries)
	require.Contains(t, strings.ToUpper(script.queries[1]), "FOR UPDATE")
	for _, query := range script.queries[1:] {
		require.Contains(t, strings.ToUpper(query), "FOR UPDATE")
	}
}

func TestTrackingCategoryDisableLocksChildrenAndAttachedPoints(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{categoryRow(3, 9, uint64Ptr(2), "detail", "Detail", "SCENE", true), categoryRow(2, 9, uint64Ptr(1), "product", "Product", "MODULE", true), categoryRow(3, 9, uint64Ptr(2), "detail", "Detail", "SCENE", true), {}, {}}
	script.execResults = []driver.Result{scriptResult{affected: 1}, scriptResult{affected: 1}}
	_, err := NewTrackingRepository(db).UpdateCategory(context.Background(), 9, 3, 1, tracking.UpdateCategory{Name: "Detail", Active: false})
	require.NoError(t, err)
	require.Contains(t, strings.ToUpper(script.queries[3]), "FOR UPDATE")
	require.Contains(t, strings.ToUpper(script.queries[4]), "FOR UPDATE")
}

func TestTrackingDraftCreationLocksActiveSceneBeforeInsert(t *testing.T) {
	script, db := openScriptedDB(t, categoryRow(3, 9, uint64Ptr(2), "detail", "Detail", "SCENE", true))
	script.execResults = []driver.Result{scriptResult{lastID: 7, affected: 1}, scriptResult{affected: 1}}
	p := tracking.Point{ProjectID: 9, CategoryID: 3, Code: "product_detail_view", Name: "Product detail", State: tracking.Draft, DevelopmentState: tracking.Pending, UVMode: tracking.Approximate, TimeBasis: tracking.Client, SupportedPlatforms: []string{"web"}, CreatedBy: 1, UpdatedBy: 1}
	_, err := NewTrackingRepository(db).CreateDraft(context.Background(), p)
	require.NoError(t, err)
	require.Contains(t, strings.ToUpper(script.queries[0]), "FOR UPDATE")
}

func TestTrackingPointListCategoryFilterIncludesFixedDepthDescendants(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{{0}, {}}
	result, err := NewTrackingRepository(db).ListPoints(context.Background(), 9, tracking.PointFilter{CategoryID: 1, Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Zero(t, result.Total)
	require.Contains(t, script.queries[0], "c.parent_id=?")
	require.Contains(t, script.queries[0], "parent.parent_id=?")
}

func TestAuthRepositorySuccessorInsertFailureRollsBackOldRevocation(t *testing.T) {
	now := time.Now().UTC()
	script, db := openScriptedDB(t, []driver.Value{uint64(7), "old-hash", now.Add(time.Hour), nil, "admin", "password-hash", "Admin", false, "ACTIVE", nil})
	script.execErrors = []error{nil, errors.New("insert successor failed")}
	repo := NewAuthRepository(db)
	successor := auth.RefreshToken{Hash: "new-hash", ExpiresAt: now.Add(7 * 24 * time.Hour)}

	_, err := repo.RotateRefreshToken(context.Background(), "old-hash", successor, now)
	require.ErrorContains(t, err, "insert successor failed")
	require.Equal(t, 1, script.rollbacks)
	require.Zero(t, script.commits)

	// The scripted database presents the old row as unrevoked again, matching
	// the state a real transactional rollback preserves.
	_, err = repo.RotateRefreshToken(context.Background(), "old-hash", successor, now)
	require.NoError(t, err)
	require.Equal(t, 1, script.commits)
}

func TestAuthRepositoryUserLookupFailureRollsBackAndOldTokenRemainsUsable(t *testing.T) {
	now := time.Now().UTC()
	script, db := openScriptedDB(t, []driver.Value{uint64(7), "old-hash", now.Add(time.Hour), nil, "admin", "password-hash", "Admin", false, "ACTIVE", nil})
	script.queryErrors = []error{errors.New("user lookup failed")}
	repo := NewAuthRepository(db)
	successor := auth.RefreshToken{Hash: "new-hash", ExpiresAt: now.Add(7 * 24 * time.Hour)}

	_, err := repo.RotateRefreshToken(context.Background(), "old-hash", successor, now)
	require.ErrorContains(t, err, "user lookup failed")
	require.Equal(t, 1, script.rollbacks)
	require.Empty(t, script.execQueries)

	_, err = repo.RotateRefreshToken(context.Background(), "old-hash", successor, now)
	require.NoError(t, err)
	require.Equal(t, 1, script.commits)
}

func TestTrackingPublishLocksDraftAndCommitsSnapshotStateAndAuditTogether(t *testing.T) {
	pointRow := trackingPointRow("DRAFT")
	propertyRow := []driver.Value{"product_id", "Product ID", "STRING", true, nil, nil, nil, nil, 0}
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{
		{uint64(3)},
		categoryRow(3, 9, uint64Ptr(2), "detail", "Detail", "SCENE", true),
		categoryRow(2, 9, uint64Ptr(1), "product", "Product", "MODULE", true),
		categoryRow(1, 9, nil, "commerce", "Commerce", "DOMAIN", true),
		categoryRow(1, 9, nil, "commerce", "Commerce", "DOMAIN", true),
		categoryRow(2, 9, uint64Ptr(1), "product", "Product", "MODULE", true),
		categoryRow(3, 9, uint64Ptr(2), "detail", "Detail", "SCENE", true),
		pointRow,
		propertyRow,
	}
	script.execResults = []driver.Result{scriptResult{lastID: 41, affected: 1}, scriptResult{affected: 1}, scriptResult{affected: 1}}

	snapshot, err := NewTrackingRepository(db).Publish(context.Background(), 9, 7, 1)
	require.NoError(t, err)
	require.Len(t, script.queries, 9)
	require.NotContains(t, strings.ToUpper(script.queries[0]), "FOR UPDATE")
	for _, index := range []int{4, 5, 6, 7} {
		require.Contains(t, strings.ToUpper(script.queries[index]), "FOR UPDATE")
	}
	require.Contains(t, script.queries[4], "tracking_category")
	require.Contains(t, script.queries[5], "tracking_category")
	require.Contains(t, script.queries[6], "tracking_category")
	require.Contains(t, script.queries[7], "tracking_point")
	require.Len(t, script.execQueries, 3)
	require.Contains(t, script.execQueries[0], "tracking_point_snapshot")
	require.Contains(t, script.execQueries[1], "definition_state='DRAFT'")
	require.Contains(t, script.execQueries[2], "audit_log")
	require.Equal(t, 1, script.commits)
	require.Zero(t, script.rollbacks)
	wantJSON, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.JSONEq(t, string(wantJSON), string(script.execArgs[0][4].Value.([]byte)))
}

func TestTrackingDraftCreationRollsBackWhenAuditInsertFails(t *testing.T) {
	script, db := openScriptedDB(t, categoryRow(3, 9, uint64Ptr(2), "detail", "Detail", "SCENE", true))
	script.execErrors = []error{nil, errors.New("audit unavailable")}
	script.execResults = []driver.Result{scriptResult{lastID: 7, affected: 1}}
	p := tracking.Point{ProjectID: 9, CategoryID: 3, Code: "product_detail_view", Name: "Product detail", BehaviorType: tracking.PageView, State: tracking.Draft, DevelopmentState: tracking.Pending, UVMode: tracking.Approximate, TimeBasis: tracking.Client, SupportedPlatforms: []string{"web"}, CreatedBy: 1, UpdatedBy: 1}
	_, err := NewTrackingRepository(db).CreateDraft(context.Background(), p)
	require.ErrorContains(t, err, "audit unavailable")
	require.Equal(t, 1, script.rollbacks)
	require.Zero(t, script.commits)
	require.Len(t, script.execQueries, 2)
	require.Contains(t, script.execQueries[1], "audit_log")
}

func TestTrackingDeprecationLocksReplacementAndRollsBackWhenAuditFails(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{trackingPointRow("PUBLISHED"), {}, trackingReplacementRow("PUBLISHED"), {}}
	script.execErrors = []error{nil, errors.New("audit unavailable")}
	script.execResults = []driver.Result{scriptResult{affected: 1}}
	replacement := uint64(8)
	err := NewTrackingRepository(db).Deprecate(context.Background(), 9, 7, 1, "replaced", &replacement)
	require.ErrorContains(t, err, "audit unavailable")
	require.Len(t, script.queries, 4)
	require.Contains(t, strings.ToUpper(script.queries[0]), "FOR UPDATE")
	require.Contains(t, strings.ToUpper(script.queries[2]), "FOR UPDATE")
	require.Equal(t, 1, script.rollbacks)
	require.Zero(t, script.commits)
}

func TestTrackingDeprecationRejectsSelfReplacement(t *testing.T) {
	_, db := openScriptedDB(t, nil)
	id := uint64(7)
	err := NewTrackingRepository(db).Deprecate(context.Background(), 9, id, 1, "self", &id)
	require.ErrorContains(t, err, "REPLACEMENT_INVALID")
}

func TestTrackingDeprecationRejectsNonPublishedReplacement(t *testing.T) {
	for _, state := range []string{"DRAFT", "DEPRECATED"} {
		t.Run(state, func(t *testing.T) {
			script, db := openScriptedDB(t, nil)
			script.queryRows = [][]driver.Value{trackingPointRow("PUBLISHED"), {}, trackingReplacementRow(state), {}}
			replacement := uint64(8)
			err := NewTrackingRepository(db).Deprecate(context.Background(), 9, 7, 1, "replaced", &replacement)
			require.ErrorContains(t, err, "REPLACEMENT_INVALID")
			require.Empty(t, script.execQueries)
			require.Equal(t, 1, script.rollbacks)
		})
	}
}
func TestTrackingDeprecationHidesMissingOrCrossProjectReplacement(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{trackingPointRow("PUBLISHED"), {}, nil}
	replacement := uint64(8)
	err := NewTrackingRepository(db).Deprecate(context.Background(), 9, 7, 1, "replaced", &replacement)
	require.ErrorIs(t, err, tracking.ErrPointNotFound)
	require.Empty(t, script.execQueries)
	require.Equal(t, 1, script.rollbacks)
}
func TestTrackingDeprecationDetectsConcurrentStateChange(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{trackingPointRow("PUBLISHED"), {}}
	script.execResults = []driver.Result{scriptResult{affected: 0}}
	err := NewTrackingRepository(db).Deprecate(context.Background(), 9, 7, 1, "old", nil)
	require.ErrorIs(t, err, tracking.ErrPointNotPublished)
	require.Equal(t, 1, script.rollbacks)
}
func TestTrackingPublicationRejectsCrossProjectCategoryUnderLock(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{{uint64(3)}, categoryRow(3, 10, uint64Ptr(2), "detail", "Detail", "SCENE", true)}
	_, err := NewTrackingRepository(db).Publish(context.Background(), 9, 7, 1)
	require.ErrorContains(t, err, "CATEGORY_INVALID")
	require.Empty(t, script.execQueries)
	require.NotContains(t, strings.ToUpper(script.queries[0]), "FOR UPDATE")
}

func TestTrackingPublicationRejectsPointCategoryChangedAfterPathLocks(t *testing.T) {
	changed := trackingPointRow("DRAFT")
	changed[2] = uint64(4)
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{{uint64(3)}, categoryRow(3, 9, uint64Ptr(2), "detail", "Detail", "SCENE", true), categoryRow(2, 9, uint64Ptr(1), "product", "Product", "MODULE", true), categoryRow(1, 9, nil, "commerce", "Commerce", "DOMAIN", true), categoryRow(1, 9, nil, "commerce", "Commerce", "DOMAIN", true), categoryRow(2, 9, uint64Ptr(1), "product", "Product", "MODULE", true), categoryRow(3, 9, uint64Ptr(2), "detail", "Detail", "SCENE", true), changed, {}}
	_, err := NewTrackingRepository(db).Publish(context.Background(), 9, 7, 1)
	require.ErrorIs(t, err, tracking.ErrConflict)
	require.Empty(t, script.execQueries)
	require.Contains(t, strings.ToUpper(script.queries[6]), "FOR UPDATE")
	require.Contains(t, strings.ToUpper(script.queries[7]), "FOR UPDATE")
}

func TestTrackingPublicationRejectsSceneDisabledBetweenReadAndLock(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{{uint64(3)}, categoryRow(3, 9, uint64Ptr(2), "detail", "Detail", "SCENE", true), categoryRow(2, 9, uint64Ptr(1), "product", "Product", "MODULE", true), categoryRow(1, 9, nil, "commerce", "Commerce", "DOMAIN", true), categoryRow(1, 9, nil, "commerce", "Commerce", "DOMAIN", true), categoryRow(2, 9, uint64Ptr(1), "product", "Product", "MODULE", true), categoryRow(3, 9, uint64Ptr(2), "detail", "Detail", "SCENE", false)}
	_, err := NewTrackingRepository(db).Publish(context.Background(), 9, 7, 1)
	require.ErrorContains(t, err, "CATEGORY_INVALID")
	require.Empty(t, script.execQueries)
	require.Contains(t, strings.ToUpper(script.queries[6]), "FOR UPDATE")
}

func TestTrackingDraftUpdateLocksAndRollsBackAuditWithBeforeAfter(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{trackingPointRow("DRAFT"), {}, categoryRow(3, 9, uint64Ptr(2), "detail", "Detail", "SCENE", true), trackingPointRow("DRAFT"), {}}
	script.execErrors = []error{nil, nil, errors.New("audit unavailable")}
	script.execResults = []driver.Result{scriptResult{affected: 1}, scriptResult{affected: 1}}
	p := tracking.Point{ID: 7, ProjectID: 9, CategoryID: 3, Category: tracking.Category{ID: 3, ProjectID: 9, Level: tracking.Scene, Active: true}, Code: "product_detail_view", Name: "Renamed", BehaviorType: tracking.PageView, State: tracking.Draft, DevelopmentState: tracking.Pending, UVMode: tracking.Approximate, TimeBasis: tracking.Client, SupportedPlatforms: []string{"web"}, UpdatedBy: 1}
	_, err := NewTrackingRepository(db).UpdateDraft(context.Background(), p)
	require.ErrorContains(t, err, "audit unavailable")
	require.NotContains(t, strings.ToUpper(script.queries[0]), "FOR UPDATE")
	require.Contains(t, strings.ToUpper(script.queries[2]), "FOR UPDATE")
	require.Contains(t, strings.ToUpper(script.queries[3]), "FOR UPDATE")
	require.Equal(t, 1, script.rollbacks)
	require.Zero(t, script.commits)
	var before, after map[string]any
	require.NoError(t, json.Unmarshal(script.execArgs[2][5].Value.([]byte), &before))
	require.NoError(t, json.Unmarshal(script.execArgs[2][6].Value.([]byte), &after))
	require.Equal(t, "Product detail", before["name"])
	require.Equal(t, "Renamed", after["name"])
}

func TestTrackingDevelopmentStateTransitionAuditsPublishedPoint(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{trackingPointRow("PUBLISHED"), {}}
	script.execResults = []driver.Result{scriptResult{affected: 1}, scriptResult{affected: 1}}
	p, err := NewTrackingRepository(db).UpdateDevelopmentState(context.Background(), 9, 7, 1, tracking.Online)
	require.NoError(t, err)
	require.Equal(t, tracking.Online, p.DevelopmentState)
	require.Contains(t, strings.ToUpper(script.queries[0]), "FOR UPDATE")
	require.Equal(t, "UPDATE_DEVELOPMENT_STATE", script.execArgs[1][2].Value)
	require.Equal(t, 1, script.commits)
}

func TestTrackingDraftUpdateAllowsZeroChangedScalarRowsForPropertyEdit(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{trackingPointRow("DRAFT"), {}, categoryRow(3, 9, uint64Ptr(2), "detail", "Detail", "SCENE", true), trackingPointRow("DRAFT"), {}}
	script.execResults = []driver.Result{scriptResult{affected: 0}, scriptResult{affected: 1}, scriptResult{affected: 1}, scriptResult{affected: 1}}
	p := tracking.Point{ID: 7, ProjectID: 9, CategoryID: 3, Category: tracking.Category{ID: 3, ProjectID: 9, Level: tracking.Scene, Active: true}, Code: "product_detail_view", Name: "Product detail", BehaviorType: tracking.PageView, State: tracking.Draft, DevelopmentState: tracking.Pending, UVMode: tracking.Approximate, TimeBasis: tracking.Client, SupportedPlatforms: []string{"web"}, Properties: []tracking.Property{{Code: "product_id", Name: "Product ID", Type: tracking.String}}, UpdatedBy: 1}
	_, err := NewTrackingRepository(db).UpdateDraft(context.Background(), p)
	require.NoError(t, err)
	require.Len(t, script.execQueries, 4)
	require.Contains(t, script.execQueries[1], "DELETE FROM tracking_point_property")
	require.Contains(t, script.execQueries[2], "INSERT INTO tracking_point_property")
	require.Contains(t, script.execQueries[3], "audit_log")
	require.Equal(t, 1, script.commits)
}

func TestTrackingDraftUnchangedUpdateIsIdempotentWithoutAudit(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{trackingPointRow("DRAFT"), {}, categoryRow(3, 9, uint64Ptr(2), "detail", "Detail", "SCENE", true), trackingPointRow("DRAFT"), {}}
	p := tracking.Point{ID: 7, ProjectID: 9, CategoryID: 3, Category: tracking.Category{ID: 3, ProjectID: 9, Level: tracking.Scene, Active: true}, Code: "product_detail_view", Name: "Product detail", BehaviorType: tracking.PageView, State: tracking.Draft, DevelopmentState: tracking.Pending, CountPV: true, CountUV: true, UVMode: tracking.Approximate, TimeBasis: tracking.Client, SupportedPlatforms: []string{"web"}, SupportedChannels: []string{}, Properties: []tracking.Property{}, CreatedBy: 1, UpdatedBy: 1}
	updated, err := NewTrackingRepository(db).UpdateDraft(context.Background(), p)
	require.NoError(t, err)
	require.Equal(t, "Product detail", updated.Name)
	require.Empty(t, script.execQueries)
	require.Equal(t, 1, script.commits)
	require.Contains(t, strings.ToUpper(script.queries[2]), "FOR UPDATE")
	require.Contains(t, strings.ToUpper(script.queries[3]), "FOR UPDATE")
}

func TestTrackingDevelopmentStateSameValueIsIdempotentWithoutAudit(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{trackingPointRow("PUBLISHED"), {}}
	p, err := NewTrackingRepository(db).UpdateDevelopmentState(context.Background(), 9, 7, 1, tracking.Pending)
	require.NoError(t, err)
	require.Equal(t, tracking.Pending, p.DevelopmentState)
	require.Empty(t, script.execQueries)
	require.Equal(t, 1, script.commits)
}

func trackingPointRow(state string) []driver.Value {
	return []driver.Value{uint64(7), uint64(9), uint64(3), "product_detail_view", "Product detail", nil, "PAGE_VIEW", state, "PENDING", nil, nil, nil, nil, true, true, "APPROXIMATE", "CLIENT", []byte(`["web"]`), []byte(`[]`), nil, nil, nil, uint64(1), nil, nil, uint64(1), uint64(1), uint64(3), uint64(9), "SCENE", true, "detail", "Detail", uint64(2)}
}

func uint64Ptr(value uint64) *uint64 { return &value }

func categoryRow(id, projectID uint64, parent *uint64, code, name, level string, active bool) []driver.Value {
	var parentValue driver.Value
	if parent != nil {
		parentValue = *parent
	}
	return []driver.Value{id, projectID, parentValue, code, name, level, nil, 0, active, uint64(1)}
}
func trackingReplacementRow(state string) []driver.Value {
	row := trackingPointRow(state)
	row[0] = uint64(8)
	row[3] = "product_detail_view_v2"
	return row
}

var (
	registerScriptedDriver sync.Once
	scriptRegistry         sync.Map
)

func openScriptedDB(t *testing.T, row []driver.Value) (*dbScript, *sql.DB) {
	t.Helper()
	registerScriptedDriver.Do(func() { sql.Register("task3-scripted", scriptedDriver{}) })
	name := t.Name()
	script := &dbScript{row: row}
	scriptRegistry.Store(name, script)
	t.Cleanup(func() { scriptRegistry.Delete(name) })
	db, err := sql.Open("task3-scripted", name)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return script, db
}

type dbScript struct {
	mu           sync.Mutex
	row          []driver.Value
	queryRows    [][]driver.Value
	queryRowSets [][][]driver.Value
	execErrors   []error
	execResults  []driver.Result
	queryErrors  []error
	queries      []string
	execQueries  []string
	execArgs     [][]driver.NamedValue
	commits      int
	rollbacks    int
}

type scriptedDriver struct{}

func (scriptedDriver) Open(name string) (driver.Conn, error) {
	value, ok := scriptRegistry.Load(name)
	if !ok {
		return nil, errors.New("missing script")
	}
	return &scriptedConn{script: value.(*dbScript)}, nil
}

type scriptedConn struct{ script *dbScript }

func (c *scriptedConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (c *scriptedConn) Close() error              { return nil }
func (c *scriptedConn) Begin() (driver.Tx, error) { return &scriptedTx{script: c.script}, nil }
func (c *scriptedConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c *scriptedConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.script.mu.Lock()
	defer c.script.mu.Unlock()
	c.script.queries = append(c.script.queries, query)
	if len(c.script.queryErrors) > 0 {
		err := c.script.queryErrors[0]
		c.script.queryErrors = c.script.queryErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	rows := [][]driver.Value{c.script.row}
	if len(c.script.queryRowSets) > 0 {
		rows = c.script.queryRowSets[0]
		c.script.queryRowSets = c.script.queryRowSets[1:]
		return &scriptedRows{rows: rows}, nil
	}
	row := c.script.row
	if len(c.script.queryRows) > 0 {
		row = c.script.queryRows[0]
		c.script.queryRows = c.script.queryRows[1:]
	}
	rows[0] = append([]driver.Value(nil), row...)
	return &scriptedRows{rows: rows}, nil
}
func (c *scriptedConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.script.mu.Lock()
	defer c.script.mu.Unlock()
	c.script.execQueries = append(c.script.execQueries, query)
	c.script.execArgs = append(c.script.execArgs, append([]driver.NamedValue(nil), args...))
	if len(c.script.execErrors) > 0 {
		err := c.script.execErrors[0]
		c.script.execErrors = c.script.execErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	if len(c.script.execResults) > 0 {
		result := c.script.execResults[0]
		c.script.execResults = c.script.execResults[1:]
		return result, nil
	}
	return driver.RowsAffected(1), nil
}

type scriptResult struct{ lastID, affected int64 }

func (r scriptResult) LastInsertId() (int64, error) { return r.lastID, nil }
func (r scriptResult) RowsAffected() (int64, error) { return r.affected, nil }

type scriptedTx struct{ script *dbScript }

func (t *scriptedTx) Commit() error {
	t.script.mu.Lock()
	defer t.script.mu.Unlock()
	t.script.commits++
	return nil
}
func (t *scriptedTx) Rollback() error {
	t.script.mu.Lock()
	defer t.script.mu.Unlock()
	t.script.rollbacks++
	return nil
}

type scriptedRows struct {
	rows [][]driver.Value
	next int
}

func (r *scriptedRows) Columns() []string {
	columnCount := 0
	if len(r.rows) > 0 {
		columnCount = len(r.rows[0])
	}
	columns := make([]string, columnCount)
	for i := range columns {
		columns[i] = "column"
	}
	return columns
}
func (r *scriptedRows) Close() error { return nil }
func (r *scriptedRows) Next(dest []driver.Value) error {
	if r.next >= len(r.rows) || len(r.rows[r.next]) == 0 {
		return io.EOF
	}
	copy(dest, r.rows[r.next])
	r.next++
	return nil
}
