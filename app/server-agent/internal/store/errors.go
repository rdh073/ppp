package store

import "errors"

var (
	ErrNotFound           = errors.New("store record not found")
	ErrCheckpointConflict = errors.New("workflow checkpoint conflict")
)
