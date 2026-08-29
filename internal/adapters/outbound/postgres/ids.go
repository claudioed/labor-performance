package postgres

import "github.com/google/uuid"

// newUUID is a small indirection so repos can mint ids without every
// caller importing google/uuid directly.
func newUUID() string {
	return uuid.NewString()
}
