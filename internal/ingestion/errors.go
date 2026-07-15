package ingestion

import (
	"errors"
	"fmt"
)

// PermanentError marks a message that cannot succeed when retried unchanged.
// Such messages are moved to the dead-letter stream immediately.
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return fmt.Sprintf("permanent ingestion failure: %v", e.Err) }
func (e *PermanentError) Unwrap() error { return e.Err }

func permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

func isPermanent(err error) bool {
	var target *PermanentError
	return errors.As(err, &target)
}
