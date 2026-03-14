package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// newID returns a random 16-byte hex string. Falls back to a timestamp on entropy failure.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// NewCommandID returns a unique command identifier with a "cmd-" prefix.
func NewCommandID() string { return "cmd-" + newID() }
