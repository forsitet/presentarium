package errs

import "errors"

var (
	ErrNotFound   = errors.New("not found")
	ErrForbidden  = errors.New("forbidden")
	ErrConflict   = errors.New("conflict")
	ErrValidation = errors.New("validation error")
	ErrUnauthorized = errors.New("unauthorized")
)

// AppError wraps an error with an HTTP status code and optional details.
type AppError struct {
	Code    int
	Message string
	Err     error
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func New(code int, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Err: err}
}

func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

func IsForbidden(err error) bool {
	return errors.Is(err, ErrForbidden)
}

func IsConflict(err error) bool {
	return errors.Is(err, ErrConflict)
}

func IsValidation(err error) bool {
	return errors.Is(err, ErrValidation)
}

func IsUnauthorized(err error) bool {
	return errors.Is(err, ErrUnauthorized)
}
