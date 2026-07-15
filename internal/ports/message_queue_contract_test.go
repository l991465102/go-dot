package ports

import "testing"

func TestDefaultDeadLetterStreamUsesGovernedName(t *testing.T) {
	if DeadLetterStream != "tracking:dead" {
		t.Fatalf("DeadLetterStream = %q, want tracking:dead", DeadLetterStream)
	}
}
