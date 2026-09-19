package db

import (
	"github.com/NewMux/mtdrb_go/internal/platform/errs"
	"github.com/NewMux/mtdrb_go/internal/platform/ids"
)

// ResolveID picks the primary key for a row a device may have created offline.
//
// A phone with no signal writes the row locally, mints a UUIDv7 for it, and
// queues the operation. Anything it writes *next* — the sets logged against a
// workout, say — references that id. If the server mints its own instead, the
// rest of the batch points at a row the server has never heard of and is
// refused, and there is no way for the outbox to rewrite a queued operation's
// foreign keys from an earlier operation's result.
//
// So the client's id wins when it supplies one. That is what UUIDv7 is for
// here: see the package comment on platform/ids.
func ResolveID(supplied *ids.ID) ids.ID {
	if supplied != nil && *supplied != (ids.ID{}) {
		return *supplied
	}
	return ids.New()
}

// ErrIDTakenElsewhere reports an id that exists but is not visible to this
// tenant.
//
// Primary keys here are the id alone, not (tenant_id, id), so accepting a
// client-supplied key means a device could name a row belonging to somebody
// else. An INSERT ... ON CONFLICT DO NOTHING would then quietly affect no rows
// and the caller would read that as a successful replay.
//
// It is not. The row is another tenant's, row-level security correctly hides
// it, and the honest answer is a conflict the trainer can see rather than a
// silent success hiding someone else's data.
func ErrIDTakenElsewhere(what string) error {
	return errs.Conflict("id_already_used",
		"that %s id is already in use; the device should mint a new one", what)
}
