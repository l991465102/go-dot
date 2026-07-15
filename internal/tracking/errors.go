package tracking

import (
	"errors"
	"fmt"
)

type CodedError struct {
	Code   string
	Detail string
}

func (e *CodedError) Error() string {
	if e.Detail == "" {
		return e.Code
	}
	return e.Code + ": " + e.Detail
}
func coded(code, format string, args ...any) error {
	return &CodedError{Code: code, Detail: fmt.Sprintf(format, args...)}
}
func NewCodedError(code, detail string) error { return &CodedError{Code: code, Detail: detail} }
func ErrorCode(err error) (string, bool) {
	var target *CodedError
	if errors.As(err, &target) {
		return target.Code, true
	}
	return "", false
}
