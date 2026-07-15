package v1

type Event struct {
	EventID     string         `json:"event_id"`
	PointCode   string         `json:"point_code"`
	EventTime   int64          `json:"event_time"`
	UserID      string         `json:"user_id,omitempty"`
	AnonymousID string         `json:"anonymous_id,omitempty"`
	SessionID   string         `json:"session_id,omitempty"`
	Platform    string         `json:"platform"`
	AppVersion  string         `json:"app_version,omitempty"`
	Channel     string         `json:"channel,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
}

type EventsReq struct {
	ProjectID uint64  `json:"project_id,omitempty"`
	Events    []Event `json:"events"`
}

type EventResult struct {
	Index   int    `json:"index"`
	EventID string `json:"event_id,omitempty"`
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type EventsRes struct {
	BatchID  string        `json:"batch_id"`
	Accepted int           `json:"accepted"`
	Rejected int           `json:"rejected"`
	Results  []EventResult `json:"results"`
}
