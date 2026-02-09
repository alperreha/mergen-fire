package manager

import "errors"

var (
	ErrInvalidRequest = errors.New("invalid request")
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("state conflict")
	ErrUnavailable    = errors.New("host dependency unavailable")
)
