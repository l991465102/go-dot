package v1

import (
	"time"

	"github.com/lzw7758/go-dot/internal/reporting"
)

type CreateReq struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	DefaultDays int      `json:"default_days"`
	PointIDs    []uint64 `json:"point_ids"`
}

func (r CreateReq) ReportInput() reporting.ReportInput {
	return reporting.ReportInput{
		Name: r.Name, Description: r.Description, DefaultDays: r.DefaultDays, PointIDs: r.PointIDs,
	}
}

type UpdateReq struct {
	Name              string     `json:"name"`
	Description       string     `json:"description"`
	DefaultDays       int        `json:"default_days"`
	PointIDs          []uint64   `json:"point_ids"`
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at"`
}

func (r UpdateReq) ReportInput() reporting.ReportInput {
	return reporting.ReportInput{
		Name: r.Name, Description: r.Description, DefaultDays: r.DefaultDays, PointIDs: r.PointIDs,
		ExpectedUpdatedAt: r.ExpectedUpdatedAt,
	}
}

type ReportRes = reporting.Report

type ReportSummary struct {
	ID          uint64    `json:"id"`
	ProjectID   uint64    `json:"project_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	DefaultDays int       `json:"default_days"`
	CreatedBy   uint64    `json:"created_by"`
	UpdatedBy   uint64    `json:"updated_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ReportsRes = []ReportSummary

func ReportSummaries(reports []reporting.Report) []ReportSummary {
	result := make([]ReportSummary, len(reports))
	for index, report := range reports {
		result[index] = ReportSummary{
			ID: report.ID, ProjectID: report.ProjectID, Name: report.Name, Description: report.Description,
			DefaultDays: report.DefaultDays, CreatedBy: report.CreatedBy, UpdatedBy: report.UpdatedBy,
			CreatedAt: report.CreatedAt, UpdatedAt: report.UpdatedAt,
		}
	}
	return result
}

type DailyQueryReq = reporting.DailyQuery
type DailyQueryRes = reporting.DailyResult
type DailyExportReq = reporting.ExportInput
