package reporting

import "errors"

var (
	ErrInvalid  = errors.New("REPORT_INVALID")
	ErrNotFound = errors.New("REPORT_NOT_FOUND")
	ErrConflict = errors.New("REPORT_CONFLICT")
)
