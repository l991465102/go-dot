package reporting

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/tracking"
	"github.com/shopspring/decimal"
)

var (
	errDailyStoreUnavailable = errors.New("reporting: daily store unavailable")
	errExporterUnavailable   = errors.New("reporting: exporter unavailable")
)

type Authorizer interface {
	RequireRole(context.Context, uint64, uint64, ...project.Role) error
}

type Service struct {
	auth       Authorizer
	repo       Repository
	daily      DailyStore
	properties PropertyDailyStore
	exporter   Exporter
	now        func() time.Time
}

func (s *Service) SetPropertyDailyStore(store PropertyDailyStore) *Service {
	s.properties = store
	return s
}

func NewService(auth Authorizer, repo Repository, daily DailyStore, exporter Exporter, now func() time.Time) *Service {
	if auth == nil {
		panic("reporting: nil authorizer")
	}
	if repo == nil {
		panic("reporting: nil repository")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{auth: auth, repo: repo, daily: daily, exporter: exporter, now: now}
}

func (s *Service) ListReports(ctx context.Context, actor, projectID uint64) ([]Report, error) {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin, project.Viewer); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, projectID)
}

func (s *Service) GetReport(ctx context.Context, actor, projectID, reportID uint64) (Report, error) {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin, project.Viewer); err != nil {
		return Report{}, err
	}
	return s.repo.Get(ctx, projectID, reportID)
}

func (s *Service) CreateReport(ctx context.Context, actor, projectID uint64, in ReportInput) (Report, error) {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin); err != nil {
		return Report{}, err
	}
	points, properties, err := s.validateInput(ctx, projectID, &in, false)
	if err != nil {
		return Report{}, err
	}
	report, err := s.repo.Create(ctx, projectID, actor, in)
	if err != nil {
		return Report{}, err
	}
	report.Points = points
	report.Properties = properties
	return report, nil
}

func (s *Service) UpdateReport(ctx context.Context, actor, projectID, reportID uint64, in ReportInput) (Report, error) {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin); err != nil {
		return Report{}, err
	}
	points, properties, err := s.validateInput(ctx, projectID, &in, true)
	if err != nil {
		return Report{}, err
	}
	report, err := s.repo.Update(ctx, projectID, reportID, actor, in)
	if err != nil {
		return Report{}, err
	}
	report.Points = points
	report.Properties = properties
	return report, nil
}

func (s *Service) DeleteReport(ctx context.Context, actor, projectID, reportID uint64) error {
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin); err != nil {
		return err
	}
	return s.repo.Delete(ctx, projectID, reportID)
}

func (s *Service) QueryDaily(ctx context.Context, actor uint64, query DailyQuery) (DailyResult, error) {
	if err := s.auth.RequireRole(ctx, actor, query.ProjectID, project.Admin, project.Viewer); err != nil {
		return DailyResult{}, err
	}
	if s.daily == nil {
		return DailyResult{}, errDailyStoreUnavailable
	}
	if query.ProjectID == 0 {
		return DailyResult{}, ErrInvalid
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return DailyResult{}, err
	}
	start, err := time.ParseInLocation(time.DateOnly, query.StartDate, location)
	if err != nil {
		return DailyResult{}, ErrInvalid
	}
	end, err := time.ParseInLocation(time.DateOnly, query.EndDate, location)
	if err != nil {
		return DailyResult{}, ErrInvalid
	}
	now := s.now().In(location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	if end.Before(start) || end.After(today) || end.After(start.AddDate(0, 0, 89)) {
		return DailyResult{}, ErrInvalid
	}
	startYear, startMonth, startDay := start.Date()
	endYear, endMonth, endDay := end.Date()
	civilStart := time.Date(startYear, startMonth, startDay, 0, 0, 0, 0, time.UTC)
	civilEnd := time.Date(endYear, endMonth, endDay, 0, 0, 0, 0, time.UTC)
	dayCount := int(civilEnd.Sub(civilStart)/(24*time.Hour)) + 1

	pointIDs := uniquePointIDs(query.PointIDs)
	if len(pointIDs) < 1 || len(pointIDs) > 100 {
		return DailyResult{}, ErrInvalid
	}
	resolved, err := s.repo.ResolvePoints(ctx, query.ProjectID, pointIDs)
	if err != nil {
		return DailyResult{}, err
	}
	byID := make(map[uint64]PointRef, len(resolved))
	for _, point := range resolved {
		if point.DefinitionState != tracking.Published && point.DefinitionState != tracking.Deprecated {
			return DailyResult{}, ErrInvalid
		}
		if point.UVMode != tracking.Approximate && point.UVMode != tracking.Exact {
			return DailyResult{}, ErrInvalid
		}
		if _, duplicate := byID[point.ID]; duplicate {
			return DailyResult{}, ErrInvalid
		}
		byID[point.ID] = point
	}
	if len(byID) != len(pointIDs) {
		return DailyResult{}, ErrInvalid
	}
	points := make([]PointRef, 0, len(pointIDs))
	filter := DailyFilter{ProjectID: query.ProjectID, Start: start, End: end.AddDate(0, 0, 1), Timezone: "Asia/Shanghai"}
	for _, id := range pointIDs {
		point, ok := byID[id]
		if !ok {
			return DailyResult{}, ErrInvalid
		}
		points = append(points, point)
		if point.UVMode == tracking.Exact {
			filter.ExactPointIDs = append(filter.ExactPointIDs, id)
		} else {
			filter.ApproximatePointIDs = append(filter.ApproximatePointIDs, id)
		}
	}

	aggregates, err := s.daily.QueryDaily(ctx, filter)
	if err != nil {
		return DailyResult{}, err
	}
	type dailyKey struct {
		date    string
		pointID uint64
	}
	indexed := make(map[dailyKey]DailyAggregate, len(aggregates))
	for _, aggregate := range aggregates {
		indexed[dailyKey{date: aggregate.Date, pointID: aggregate.PointID}] = aggregate
	}
	result := DailyResult{
		Timezone: "Asia/Shanghai", StartDate: query.StartDate, EndDate: query.EndDate,
		PointCount: len(points), DayCount: dayCount, Items: make([]DailyItem, 0, dayCount*len(points)),
	}
	for date := end; !date.Before(start); date = date.AddDate(0, 0, -1) {
		dateText := date.Format(time.DateOnly)
		complete := date.Before(today)
		for _, point := range points {
			aggregate := indexed[dailyKey{date: dateText, pointID: point.ID}]
			result.Items = append(result.Items, DailyItem{
				Date: dateText, Complete: complete, PointID: point.ID, PointCode: point.Code,
				PointName: point.Name, PV: aggregate.PV, UV: aggregate.UV, UVMode: point.UVMode,
			})
			result.TotalPV += aggregate.PV
		}
		if !complete {
			result.IncompleteDates = append(result.IncompleteDates, dateText)
		}
	}
	if len(query.PropertySelections) > 0 {
		if s.properties == nil {
			return DailyResult{}, errDailyStoreUnavailable
		}
		propertyRefs, err := s.resolvePropertySelections(ctx, query.ProjectID, pointIDs, query.PropertySelections)
		if err != nil {
			return DailyResult{}, err
		}
		data, err := s.properties.QueryPropertyDaily(ctx, PropertyDailyFilter{
			ProjectID: query.ProjectID, Start: start, End: end.AddDate(0, 0, 1), Timezone: "Asia/Shanghai",
		}, propertyRefs)
		if err != nil {
			return DailyResult{}, err
		}
		result.MetricSections, result.DimensionSections = buildPropertySections(start, end, today, propertyRefs, data)
	}
	return result, nil
}

func (s *Service) ExportDaily(ctx context.Context, actor uint64, username string, in ExportInput) (ExportFile, error) {
	result, err := s.QueryDaily(ctx, actor, in.DailyQuery)
	if err != nil {
		return ExportFile{}, err
	}
	if s.exporter == nil {
		return ExportFile{}, errExporterUnavailable
	}
	in.ReportName = strings.TrimSpace(in.ReportName)
	in.ReportDescription = strings.TrimSpace(in.ReportDescription)
	if utf8.RuneCountInString(in.ReportName) > 128 || utf8.RuneCountInString(in.ReportDescription) > 1000 {
		return ExportFile{}, ErrInvalid
	}
	contents, err := s.exporter.Build(ExportDocument{
		Result: result, ReportName: in.ReportName, ReportDescription: in.ReportDescription,
		ExportedBy: username, ExportedAt: s.now().UTC(),
	})
	if err != nil {
		return ExportFile{}, err
	}
	return ExportFile{Filename: exportFilename(in.ReportName, result.StartDate, result.EndDate), Contents: contents}, nil
}

func exportFilename(reportName, startDate, endDate string) string {
	name := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == '"' || r == '\'' || r == ';' || unicode.IsControl(r) {
			return -1
		}
		return r
	}, reportName)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "自定义报表"
	}
	return fmt.Sprintf("%s_%s_%s.xlsx", name, strings.ReplaceAll(startDate, "-", ""), strings.ReplaceAll(endDate, "-", ""))
}

func (s *Service) validateInput(ctx context.Context, projectID uint64, in *ReportInput, requireExpectedUpdatedAt bool) ([]PointRef, []PropertyRef, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(in.Name) == 0 || utf8.RuneCountInString(in.Name) > 128 || utf8.RuneCountInString(in.Description) > 1000 {
		return nil, nil, ErrInvalid
	}
	if in.DefaultDays < 1 || in.DefaultDays > 90 || (requireExpectedUpdatedAt && in.ExpectedUpdatedAt == nil) {
		return nil, nil, ErrInvalid
	}

	in.PointIDs = uniquePointIDs(in.PointIDs)
	if len(in.PointIDs) < 1 || len(in.PointIDs) > 100 {
		return nil, nil, ErrInvalid
	}
	resolved, err := s.repo.ResolvePoints(ctx, projectID, in.PointIDs)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[uint64]PointRef, len(resolved))
	for _, point := range resolved {
		if point.DefinitionState != tracking.Published && point.DefinitionState != tracking.Deprecated {
			return nil, nil, ErrInvalid
		}
		if _, duplicate := byID[point.ID]; duplicate {
			return nil, nil, ErrInvalid
		}
		byID[point.ID] = point
	}
	if len(byID) != len(in.PointIDs) {
		return nil, nil, ErrInvalid
	}
	points := make([]PointRef, len(in.PointIDs))
	for i, id := range in.PointIDs {
		point, ok := byID[id]
		if !ok {
			return nil, nil, ErrInvalid
		}
		point.SortNo = i
		points[i] = point
	}
	properties, err := s.resolvePropertySelections(ctx, projectID, in.PointIDs, in.PropertySelections)
	if err != nil {
		return nil, nil, err
	}
	in.PropertySelections = make([]PropertySelection, len(properties))
	for i, property := range properties {
		in.PropertySelections[i] = property.PropertySelection
	}
	return points, properties, nil
}

func (s *Service) resolvePropertySelections(ctx context.Context, projectID uint64, pointIDs []uint64, selections []PropertySelection) ([]PropertyRef, error) {
	if len(selections) == 0 {
		return []PropertyRef{}, nil
	}
	if len(selections) > 20 {
		return nil, ErrInvalid
	}
	allowedPoints := make(map[uint64]struct{}, len(pointIDs))
	for _, id := range pointIDs {
		allowedPoints[id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(selections))
	normalized := make([]PropertySelection, len(selections))
	for i, selection := range selections {
		selection.PropertyCode = strings.TrimSpace(selection.PropertyCode)
		key := fmt.Sprintf("%d:%s", selection.PointID, selection.PropertyCode)
		if selection.PointID == 0 || selection.PropertyCode == "" {
			return nil, ErrInvalid
		}
		if _, ok := allowedPoints[selection.PointID]; !ok {
			return nil, ErrInvalid
		}
		if _, duplicate := seen[key]; duplicate {
			return nil, ErrInvalid
		}
		seen[key] = struct{}{}
		selection.SortNo = i
		normalized[i] = selection
	}
	propertyRepo, ok := s.repo.(PropertyRepository)
	if !ok {
		return nil, errDailyStoreUnavailable
	}
	resolved, err := propertyRepo.ResolveProperties(ctx, projectID, normalized)
	if err != nil {
		return nil, err
	}
	if len(resolved) != len(normalized) {
		return nil, ErrInvalid
	}
	dimensions := 0
	for i := range resolved {
		ref := &resolved[i]
		ref.SortNo = i
		switch ref.Analytics.Mode {
		case tracking.AnalyticsMetric:
			if len(ref.Aggregations) == 0 {
				return nil, ErrInvalid
			}
			allowed := make(map[tracking.MetricAggregation]struct{}, len(ref.Analytics.Aggregations))
			for _, aggregation := range ref.Analytics.Aggregations {
				allowed[aggregation] = struct{}{}
			}
			selected := make(map[tracking.MetricAggregation]struct{}, len(ref.Aggregations))
			for _, aggregation := range ref.Aggregations {
				if _, ok := allowed[aggregation]; !ok {
					return nil, ErrInvalid
				}
				if _, duplicate := selected[aggregation]; duplicate {
					return nil, ErrInvalid
				}
				selected[aggregation] = struct{}{}
			}
		case tracking.AnalyticsDimension:
			dimensions++
			if len(ref.Aggregations) != 0 {
				return nil, ErrInvalid
			}
		default:
			return nil, ErrInvalid
		}
	}
	if dimensions > 5 {
		return nil, ErrInvalid
	}
	return resolved, nil
}

func buildPropertySections(start, end, today time.Time, refs []PropertyRef, data PropertyDailyData) ([]MetricSection, []DimensionSection) {
	metricRefs := make([]PropertyRef, 0)
	dimensionRefs := make([]PropertyRef, 0)
	for _, ref := range refs {
		if ref.Analytics.Mode == tracking.AnalyticsMetric {
			metricRefs = append(metricRefs, ref)
		} else {
			dimensionRefs = append(dimensionRefs, ref)
		}
	}
	metricIndex := make(map[string]MetricAggregate, len(data.Metrics))
	for _, aggregate := range data.Metrics {
		metricIndex[fmt.Sprintf("%s:%d:%s", aggregate.Date, aggregate.PointID, aggregate.PropertyCode)] = aggregate
	}
	metric := MetricSection{}
	for _, ref := range metricRefs {
		for _, aggregation := range ref.Aggregations {
			key := metricColumnKey(ref, aggregation)
			metric.Columns = append(metric.Columns, MetricColumn{
				Key: key, Label: key, PointCode: ref.PointCode, PropertyCode: ref.PropertyCode, Aggregation: aggregation,
			})
		}
	}
	if len(metric.Columns) > 0 {
		for date := end; !date.Before(start); date = date.AddDate(0, 0, -1) {
			dateText := date.Format(time.DateOnly)
			row := MetricRow{Date: dateText, Complete: date.Before(today), Values: make(map[string]*string)}
			for _, ref := range metricRefs {
				aggregate := metricIndex[fmt.Sprintf("%s:%d:%s", dateText, ref.PointID, ref.PropertyCode)]
				for _, aggregation := range ref.Aggregations {
					row.Values[metricColumnKey(ref, aggregation)] = metricValue(aggregate, aggregation)
				}
			}
			metric.Rows = append(metric.Rows, row)
		}
	}

	dimensions := make([]DimensionSection, 0, len(dimensionRefs))
	for _, ref := range dimensionRefs {
		section := DimensionSection{
			PointID: ref.PointID, SortNo: ref.SortNo, PointCode: ref.PointCode,
			PropertyCode: ref.PropertyCode, PropertyName: ref.PropertyName,
		}
		byDate := make(map[string][]DimensionAggregate)
		for _, aggregate := range data.Dimensions {
			if aggregate.PointID == ref.PointID && aggregate.PropertyCode == ref.PropertyCode {
				byDate[aggregate.Date] = append(byDate[aggregate.Date], aggregate)
			}
		}
		for date := end; !date.Before(start); date = date.AddDate(0, 0, -1) {
			dateText := date.Format(time.DateOnly)
			values := byDate[dateText]
			if ref.Type == tracking.Enum || ref.Type == tracking.Boolean {
				expected := ref.EnumValues
				if ref.Type == tracking.Boolean {
					expected = []string{"true", "false"}
				}
				existing := make(map[string]struct{}, len(values))
				for _, value := range values {
					existing[value.Value] = struct{}{}
				}
				for _, value := range expected {
					if _, ok := existing[value]; !ok {
						values = append(values, DimensionAggregate{Date: dateText, PointID: ref.PointID, PropertyCode: ref.PropertyCode, Value: value})
					}
				}
			}
			sort.SliceStable(values, func(i, j int) bool {
				if values[i].EventCount == values[j].EventCount {
					return values[i].Value < values[j].Value
				}
				return values[i].EventCount > values[j].EventCount
			})
			var total uint64
			for _, value := range values {
				total += value.EventCount
			}
			for _, value := range values {
				share := float64(0)
				if total > 0 {
					share = float64(value.EventCount) / float64(total)
				}
				section.Rows = append(section.Rows, DimensionRow{
					Date: dateText, Complete: date.Before(today), Value: value.Value,
					EventCount: value.EventCount, UV: value.UV, Share: share,
				})
			}
		}
		dimensions = append(dimensions, section)
	}
	metricSections := []MetricSection{}
	if len(metric.Columns) > 0 {
		metricSections = append(metricSections, metric)
	}
	return metricSections, dimensions
}

func metricColumnKey(ref PropertyRef, aggregation tracking.MetricAggregation) string {
	return strings.ToLower(fmt.Sprintf("%s_%s_%s", ref.PointCode, ref.PropertyCode, aggregation))
}

func metricValue(aggregate MetricAggregate, aggregation tracking.MetricAggregation) *string {
	var value string
	switch aggregation {
	case tracking.AggregateCount:
		value = strconv.FormatUint(aggregate.Count, 10)
	case tracking.AggregateSum:
		if aggregate.Count == 0 {
			value = "0"
		} else {
			value = aggregate.Sum
		}
	case tracking.AggregateAvg:
		if aggregate.Count == 0 {
			return nil
		}
		sum, err := decimal.NewFromString(aggregate.Sum)
		if err != nil {
			return nil
		}
		value = sum.Div(decimal.NewFromInt(int64(aggregate.Count))).String()
	case tracking.AggregateMin:
		if aggregate.Count == 0 {
			return nil
		}
		value = aggregate.Min
	case tracking.AggregateMax:
		if aggregate.Count == 0 {
			return nil
		}
		value = aggregate.Max
	default:
		return nil
	}
	return &value
}

func uniquePointIDs(ids []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(ids))
	unique := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}
