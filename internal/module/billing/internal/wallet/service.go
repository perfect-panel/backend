// Package wallet implements the wallet subdomain of the billing module: the
// user-facing commission withdrawal, balance/commission statements and
// affiliate earnings overview, and the wallet reads and money movements
// other modules request from billing (ADR-001 rules 2 and 4) — the
// administrator's wallet edits, cancellation refunds and quota-task gifts,
// each committed in a billing transaction of its own. Only the module facade
// may reach it.
package wallet

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/repository"
)

// LogReader is the read side of the audit log the statements and the
// affiliate overview show: the user's balance and commission entries and the
// net commission they add up to.
type LogReader interface {
	FilterSystemLog(ctx context.Context, filter *log.FilterParams) ([]*log.SystemLog, int64, error)
	SumAmountByTypeAndObjectID(ctx context.Context, typ uint8, objectID int64) (int64, error)
}

// Transactor mirrors the facade's billing-scoped transaction port.
type Transactor interface {
	InBillingTx(ctx context.Context, fn func(repository.BillingStore) error) error
}

// AffiliateReader is the read-only port onto the identity domain's referral
// relations; the legacy user repository satisfies it structurally. The
// commission amounts belong to billing, the referral tree stays with
// identity.
type AffiliateReader interface {
	CountAffiliates(ctx context.Context, refererId int64) (int64, error)
	QueryAffiliateList(ctx context.Context, refererId int64, page, size int) ([]*user.User, int64, error)
}

// AuthMethodReader is the read-only identity port used to render an
// affiliate's masked login identifier.
type AuthMethodReader interface {
	FindUserAuthMethods(ctx context.Context, userId int64) ([]*user.AuthMethods, error)
}

// ProfileReader is the read-only identity port resolving the referrer a
// refund takes the commission back from.
type ProfileReader interface {
	FindOne(ctx context.Context, id int64) (*user.User, error)
}

// Store is the persistence the movements other modules request need: the
// billing-scoped transactions, the wallet view for plain reads and the inbox
// holding the movements' idempotency markers.
type Store interface {
	Transactor
	Inbox() repository.InboxRepo
	Wallet() repository.WalletRepo
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Logs        LogReader
	Withdrawals repository.UserWithdrawalRepo
	Affiliates  AffiliateReader
	AuthMethods AuthMethodReader
	Tx          Transactor
	// Store and Profiles serve the reads and movements other modules
	// request.
	Store    Store
	Profiles ProfileReader
}

// Service is the wallet subdomain entry point used by the billing facade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}
