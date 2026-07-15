package ports

import "testing"

func TestDefaultDeadLetterStreamUsesGovernedName(t *testing.T) {
	if DeadLetterStream != "tracking:{queue}:dead" {
		t.Fatalf("DeadLetterStream = %q, want tracking:{queue}:dead", DeadLetterStream)
	}
}
