// Package ids issues UUIDv7 identifiers.
//
// v7 is time-ordered, which matters twice here: offline clients mint their own
// primary keys and those rows must still land in a sane index order on sync,
// and ledger rows read chronologically without a secondary sort.
package ids

import (
	"encoding/binary"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// ID is a UUID primary key.
type ID = uuid.UUID

// New returns a fresh time-ordered identifier.
func New() ID {
	if next := sequence.Load(); next != nil {
		return (*next)()
	}
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

var sequence atomic.Pointer[func() ID]

// UseSequence makes New return a reproducible sequence of UUIDv7s, stamped
// from start and one millisecond apart, until restore is called.
//
// For cmd/demo only. Its recording is checked in and re-made by every slice
// that extends the scenario; with random ids each re-recording would rewrite
// every line of it, and the diff that should show what changed would show
// nothing useful. Never for a server: two processes on one sequence would
// mint the same ids.
func UseSequence(start time.Time, seed int64) (restore func()) {
	var mu sync.Mutex
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // reproducible by design; see above
	ms := start.UnixMilli()
	generate := func() ID {
		mu.Lock()
		defer mu.Unlock()
		var id ID
		binary.BigEndian.PutUint64(id[0:8], uint64(ms)<<16)
		binary.BigEndian.PutUint64(id[8:16], rng.Uint64())
		// The low 16 bits of the first word carry random bits, then the
		// version and variant overwrite their nibbles.
		binary.BigEndian.PutUint16(id[6:8], uint16(rng.Uint32()))
		id[6] = id[6]&0x0f | 0x70
		id[8] = id[8]&0x3f | 0x80
		ms++
		return id
	}
	sequence.Store(&generate)
	return func() { sequence.Store(nil) }
}
