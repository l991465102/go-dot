package v1

import "github.com/lzw7758/go-dot/internal/tracking"

type SaveDraftReq = tracking.DraftInput
type PointRes = tracking.Point
type SnapshotRes = tracking.Snapshot
type CopyReq struct {
	Code string `json:"code"`
}
type DeprecateReq struct {
	Reason        string  `json:"reason"`
	ReplacementID *uint64 `json:"replacement_id,omitempty"`
}
type DevelopmentStateReq struct {
	State tracking.DevelopmentState `json:"state"`
}
