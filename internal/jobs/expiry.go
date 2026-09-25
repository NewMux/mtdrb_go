package jobs

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/NewMux/mtdrb_go/internal/billing"
)

// PackageExpiry retires packs whose expiry date has passed with credits left.
//
// The billing engine has always known how — recognise the unused value as
// revenue, since the training will now never be owed — and nothing ever
// called it, so an expired pack sat in Deferred Revenue for ever, overstating
// what the trainer owed. It runs a quarter of an hour after the tenant's
// midnight: a pack that expires "on the 30th" is good for the whole of the
// 30th where the trainer is, and gone on the 31st.
func PackageExpiry(b *billing.Service) Job {
	return Daily{
		JobName: "package_expiry",
		At:      15 * time.Minute,
		Do: func(ctx context.Context, tx pgx.Tx, t Tenant, now time.Time) (int, error) {
			return b.ExpirePackages(ctx, tx, t.ID, t.Today(now), nil)
		},
	}
}
