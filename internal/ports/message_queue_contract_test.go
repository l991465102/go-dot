package ports

import "testing"

func TestDLQStreamUsesGovernedDeadLetterStream(t *testing.T) {
	if got := DLQStream("tracking:events"); got != DeadLetterStream {
		t.Fatalf("DLQStream() = %q, want %q", got, DeadLetterStream)
	}
}
