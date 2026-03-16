package store

import "errors"

var (
	ErrNotFound          = errors.New("not found")
	ErrDuplicateEmail    = errors.New("duplicate email")
	ErrDuplicateUsername = errors.New("duplicate username")
)
