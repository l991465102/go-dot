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

type Report struct {
	ID          uint64     `json:"id"`
	ProjectID   uint64     `json:"project_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	DefaultDays int        `json:"default_days"`
	Points      []PointRef `json:"points"`
	CreatedBy   uint64     `json:"created_by"`
	UpdatedBy   uint64     `json:"updated_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type ReportInput struct {
	Name              string     `json:"name"`
	Description       string     `json:"description"`
	DefaultDays       int        `json:"default_days"`
	PointIDs          []uint64   `json:"point_ids"`
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}

type Repository interface {
	List(context.Context, uint64) ([]Report, error)
	Get(context.Context, uint64, uint64) (Report, error)
	Create(context.Context, uint64, uint64, ReportInput) (Report, error)
	Update(context.Context, uint64, uint64, uint64, ReportInput) (Report, error)
	Delete(context.Context, uint64, uint64) error
	ResolvePoints(context.Context, uint64, []uint64) ([]PointRef, error)
}

type DailyQuery struct {
	ProjectID uint64   `json:"project_id"`
	StartDate string   `json:"start_date"`
	EndDate   string   `json:"end_date"`
	PointIDs  []uint64 `json:"point_ids"`
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
	Timezone   string      `json:"timezone"`
	StartDate  string      `json:"start_date"`
	EndDate    string      `json:"end_date"`
	PointCount int         `json:"point_count"`
	DayCount   int         `json:"day_count"`
	TotalPV    uint64      `json:"total_pv"`
	Items      []DailyItem `json:"items"`
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
