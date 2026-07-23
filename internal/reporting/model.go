package reporting

import (
	"context"
	"time"

	"github.com/lzw7758/go-dot/internal/tracking"
)

type PointRef struct {
	ID              uint64                   `json:"id"`
	Code            string                   `json:"code"`
	Name            string                   `json:"name"`
	DefinitionState tracking.DefinitionState `json:"definition_state"`
	UVMode          tracking.UVMode          `json:"uv_mode"`
	SortNo          int                      `json:"sort_no"`
}

type PropertySelection struct {
	PointID      uint64                       `json:"point_id"`
	PropertyCode string                       `json:"property_code"`
	Aggregations []tracking.MetricAggregation `json:"aggregations,omitempty"`
	SortNo       int                          `json:"sort_no,omitempty"`
}

type PropertyRef struct {
	PropertySelection
	PointCode    string                     `json:"point_code"`
	PropertyName string                     `json:"property_name"`
	Type         tracking.PropertyType      `json:"type"`
	Analytics    tracking.PropertyAnalytics `json:"analytics"`
	EnumValues   []string                   `json:"enum_values,omitempty"`
	UVMode       tracking.UVMode            `json:"uv_mode"`
}

type Report struct {
	ID          uint64        `json:"id"`
	ProjectID   uint64        `json:"project_id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	DefaultDays int           `json:"default_days"`
	Points      []PointRef    `json:"points"`
	Properties  []PropertyRef `json:"properties"`
	CreatedBy   uint64        `json:"created_by"`
	UpdatedBy   uint64        `json:"updated_by"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type ReportInput struct {
	Name               string              `json:"name"`
	Description        string              `json:"description"`
	DefaultDays        int                 `json:"default_days"`
	PointIDs           []uint64            `json:"point_ids"`
	PropertySelections []PropertySelection `json:"property_queries"`
	ExpectedUpdatedAt  *time.Time          `json:"expected_updated_at,omitempty"`
}

type Repository interface {
	List(context.Context, uint64) ([]Report, error)
	Get(context.Context, uint64, uint64) (Report, error)
	Create(context.Context, uint64, uint64, ReportInput) (Report, error)
	Update(context.Context, uint64, uint64, uint64, ReportInput) (Report, error)
	Delete(context.Context, uint64, uint64) error
	ResolvePoints(context.Context, uint64, []uint64) ([]PointRef, error)
}

type PropertyRepository interface {
	ResolveProperties(context.Context, uint64, []PropertySelection) ([]PropertyRef, error)
}

type DailyQuery struct {
	ProjectID          uint64              `json:"project_id"`
	StartDate          string              `json:"start_date"`
	EndDate            string              `json:"end_date"`
	PointIDs           []uint64            `json:"point_ids"`
	PropertySelections []PropertySelection `json:"property_queries"`
}

type DailyFilter struct {
	ProjectID           uint64
	Start               time.Time
	End                 time.Time
	Timezone            string
	ApproximatePointIDs []uint64
	ExactPointIDs       []uint64
}

type DailyAggregate struct {
	Date      string
	PointID   uint64
	PointCode string
	PV        uint64
	UV        uint64
}

type DailyStore interface {
	QueryDaily(context.Context, DailyFilter) ([]DailyAggregate, error)
}

type DailyItem struct {
	Date      string          `json:"date"`
	Complete  bool            `json:"complete"`
	PointID   uint64          `json:"point_id"`
	PointCode string          `json:"point_code"`
	PointName string          `json:"point_name"`
	PV        uint64          `json:"pv"`
	UV        uint64          `json:"uv"`
	UVMode    tracking.UVMode `json:"uv_mode"`
}

type DailyResult struct {
	Timezone          string             `json:"timezone"`
	StartDate         string             `json:"start_date"`
	EndDate           string             `json:"end_date"`
	PointCount        int                `json:"point_count"`
	DayCount          int                `json:"day_count"`
	TotalPV           uint64             `json:"total_pv"`
	Items             []DailyItem        `json:"items"`
	MetricSections    []MetricSection    `json:"metric_sections,omitempty"`
	DimensionSections []DimensionSection `json:"dimension_sections,omitempty"`
	IncompleteDates   []string           `json:"incomplete_dates,omitempty"`
}

type PropertyDailyFilter struct {
	ProjectID uint64
	Start     time.Time
	End       time.Time
	Timezone  string
}

type MetricAggregate struct {
	Date, PointCode, PropertyCode string
	PointID                       uint64
	Count                         uint64
	Sum, Min, Max                 string
	Present                       bool
}

type DimensionAggregate struct {
	Date, PointCode, PropertyCode, Value string
	PointID                              uint64
	EventCount, UV                       uint64
}

type PropertyDailyData struct {
	Metrics        []MetricAggregate
	Dimensions     []DimensionAggregate
	CommittedHours []time.Time
}

type PropertyDailyStore interface {
	QueryPropertyDaily(context.Context, PropertyDailyFilter, []PropertyRef) (PropertyDailyData, error)
}

type MetricColumn struct {
	Key          string                     `json:"key"`
	Label        string                     `json:"label"`
	PointCode    string                     `json:"point_code"`
	PropertyCode string                     `json:"property_code"`
	Aggregation  tracking.MetricAggregation `json:"aggregation"`
}

type MetricRow struct {
	Date     string             `json:"date"`
	Complete bool               `json:"complete"`
	Values   map[string]*string `json:"values"`
}

type MetricSection struct {
	Columns []MetricColumn `json:"columns"`
	Rows    []MetricRow    `json:"rows"`
}

type DimensionRow struct {
	Date       string  `json:"date"`
	Complete   bool    `json:"complete"`
	Value      string  `json:"value"`
	EventCount uint64  `json:"event_count"`
	UV         uint64  `json:"uv"`
	Share      float64 `json:"share"`
}

type DimensionSection struct {
	PointID      uint64         `json:"point_id"`
	SortNo       int            `json:"sort_no"`
	PointCode    string         `json:"point_code"`
	PropertyCode string         `json:"property_code"`
	PropertyName string         `json:"property_name"`
	Rows         []DimensionRow `json:"rows"`
}

type ExportDocument struct {
	Result            DailyResult
	ReportName        string
	ReportDescription string
	ExportedBy        string
	ExportedAt        time.Time
}

type Exporter interface {
	Build(ExportDocument) ([]byte, error)
}

type ExportInput struct {
	DailyQuery
	ReportName        string `json:"report_name"`
	ReportDescription string `json:"report_description"`
}

type ExportFile struct {
	Filename string
	Contents []byte
}
