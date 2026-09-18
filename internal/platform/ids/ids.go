// Package ids issues UUIDv7 identifiers.
//
// v7 is time-ordered, which matters twice here: offline clients mint their own
// primary keys and those rows must still land in a sane index order on sync,
// and ledger rows read chronologically without a secondary sort.
package ids

import (
	"github.com/google/uuid"
)

// ID is a UUID primary key.
type ID = uuid.UUID

// New returns a fresh time-ordered identifier.
func New() ID {
	id, err := uuid.NewV7()
	if err != nil {
		// NewV7 only fails if the system entropy source fails, in which case
		// nothing downstream is trustworthy either.
		panic("ids: cannot generate UUIDv7: " + err.Error())
	}
	return id
}

// Parse validates a client-supplied identifier.
func Parse(s string) (ID, error) { return uuid.Parse(s) }

// Nil is the zero identifier.
var Nil = uuid.Nil
