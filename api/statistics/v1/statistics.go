package v1

import (
	"github.com/lzw7758/go-dot/internal/ports"
)

type SummaryRes = ports.Summary
type TrendRes struct {
	Items []ports.TrendPoint `json:"items"`
}
