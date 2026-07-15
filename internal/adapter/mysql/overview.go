package mysql

import (
	"context"
	"fmt"

	"github.com/lzw7758/go-dot/internal/overview"
)

func (r *TrackingRepository) PublishedPoints(ctx context.Context, projectID uint64) ([]overview.PublishedPoint, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,code,name,published_at FROM tracking_point WHERE project_id=? AND definition_state='PUBLISHED' ORDER BY published_at,id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query published points: %w", err)
	}
	defer rows.Close()
	items := []overview.PublishedPoint{}
	for rows.Next() {
		var item overview.PublishedPoint
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.PublishedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
