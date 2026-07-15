package ingestion

const (
	EventStream        = "tracking:{queue}:events"
	InvalidEventStream = "tracking:{queue}:invalid"
	DeadLetterStream   = "tracking:{queue}:dead"
	WorkerGroup        = "tracking-workers"
)
