package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/lzw7758/go-dot/internal/ingestion"
	"github.com/lzw7758/go-dot/internal/project"
)

type TestEventReader interface {
	RecentTestRecords(context.Context, uint64, string) ([]ingestion.TestRecord, error)
}
type TestEventsController struct {
	reader TestEventReader
	auth   *project.Service
}

// NewTestEventsController 创建测试事件查询接口。
func NewTestEventsController(reader TestEventReader, auth *project.Service) *TestEventsController {
	return &TestEventsController{reader: reader, auth: auth}
}

// Register 注册项目内测试事件查询接口。
func (c *TestEventsController) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/projects/{projectID}/test-events", c.recent)
}

// recent 查询项目内的测试采集记录。
func (c *TestEventsController) recent(w http.ResponseWriter, r *http.Request) {
	actor, p, ok := principalID(w, r)
	if !ok {
		return
	}
	if err := c.auth.RequireRole(r.Context(), actor, p, project.Admin, project.Viewer); err != nil {
		trackingError(w, err)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("point_code"))
	if code == "" || len(code) > 128 {
		writeError(w, 400, "invalid point code")
		return
	}
	records, err := c.reader.RecentTestRecords(r.Context(), p, code)
	if err != nil {
		writeError(w, 500, "test events unavailable")
		return
	}
	writeJSON(w, 200, records)
}
