package v1

import "time"

type OverviewWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}
type OverviewQueue struct {
	Pending                 int64 `json:"pending"`
	OldestPendingAgeSeconds int64 `json:"oldest_pending_age_seconds"`
}
type OverviewPoint struct {
	ID          uint64    `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	PublishedAt time.Time `json:"published_at"`
}
type OverviewRes struct {
	Window                 OverviewWindow  `json:"window"`
	InvalidCount           int64           `json:"invalid_count"`
	DeadLetterCount        int64           `json:"dead_letter_count"`
	Queue                  OverviewQueue   `json:"queue"`
	NoReportPoints         []OverviewPoint `json:"no_report_points"`
	NoReportThresholdHours int             `json:"no_report_threshold_hours"`
}
