package contracts

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Errors carry a stable code (multilingual spec §10), such as
// MODEL_NOT_INSTALLED, and the values a message needs, such as model_id.
// Clients show text for the code from their catalog
// (i18n/locales/<language>/errors.json), in the App language. The English
// message stays with the error for logs, Diagnostics, and clients that do not
// know the code yet.
//
// Codes are part of the client contract: a code keeps its meaning, and a new,
// more specific code may replace a general one such as BAD_REQUEST. Every
// code core sends has an English entry in errors.json; a test checks it.

// CodedError is an error with a stable code and the values its message needs.
type CodedError struct {
	Code   string
	Params map[string]any
	Err    error
}

func (e *CodedError) Error() string { return e.Err.Error() }

func (e *CodedError) Unwrap() error { return e.Err }

// NewError gives err a stable code. Params are the values the message needs,
// sent to clients as the error's details.
func NewError(code string, params map[string]any, err error) *CodedError {
	return &CodedError{Code: code, Params: params, Err: err}
}

// Errorf is NewError with a formatted English message. %w wraps as fmt.Errorf does.
func Errorf(code string, params map[string]any, format string, args ...any) error {
	return &CodedError{Code: code, Params: params, Err: fmt.Errorf(format, args...)}
}

// ErrorCode is the code of the first coded error in err's chain, and its
// params; "" when there is none.
func ErrorCode(err error) (string, map[string]any) {
	var coded *CodedError
	if errors.As(err, &coded) {
		return coded.Code, coded.Params
	}
	return "", nil
}

// Failure is an error as a record keeps it for people to read (#588): its
// English text, its stable code, and the code's values.
type Failure struct {
	Text    string
	Code    string
	Details map[string]any
}

// FailureOf is err as a Failure, with its own code, or fallback when it has
// none, so every failure a record shows carries a code.
func FailureOf(err error, fallback string) Failure {
	if err == nil {
		return Failure{}
	}
	code, details := ErrorCode(err)
	if code == "" {
		code = fallback
	}
	return Failure{Text: err.Error(), Code: code, Details: details}
}

// DetailsColumn is f's details for a database column: JSON, or nil when
// there are none.
func (f Failure) DetailsColumn() any {
	if len(f.Details) == 0 {
		return nil
	}
	data, err := json.Marshal(f.Details)
	if err != nil {
		return nil
	}
	return string(data)
}

// CodeColumn is f's code for a database column, nil when there is none.
func (f Failure) CodeColumn() any {
	if f.Code == "" {
		return nil
	}
	return f.Code
}

// ParseErrorDetails reads a details column; nil when it is empty or unreadable.
func ParseErrorDetails(column string) map[string]any {
	if column == "" {
		return nil
	}
	var details map[string]any
	if json.Unmarshal([]byte(column), &details) != nil {
		return nil
	}
	return details
}
