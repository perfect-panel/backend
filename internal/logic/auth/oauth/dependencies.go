package oauth

import (
	"context"

	"github.com/perfect-panel/server/internal/repository"
	"github.com/redis/go-redis/v9"
)

// OAuthRegistrationPolicy isolates registration policy enforcement from the
// OAuth use case's infrastructure composition.
type OAuthRegistrationPolicy interface {
	EnsureMethodEnabled(ctx context.Context, method string) error
	EnsureRegistrationOpen(ctx context.Context, method string) error
	VerifyHuman(ctx context.Context, token, ip string) error
	TakeIPPermit(ctx context.Context, ip string) error
}

// OAuthLoginStore is the persistence surface required by OAuth login and
// registration. It intentionally excludes unrelated Store domains.
type OAuthLoginStore interface {
	Auth() repository.AuthRepo
	User() repository.UserRepo
	UserAuth() repository.UserAuthRepo
	UserSubscription() repository.UserSubscriptionRepo
	UserCache() repository.UserCacheRepo
	Subscribe() repository.SubscribeRepo
	Log() repository.LogRepo
	InTx(ctx context.Context, fn func(repository.Store) error) error
}

// OAuthLoginConfig is the configuration snapshot consumed by the OAuth login
// and registration flow.
type OAuthLoginConfig struct {
	InviteForced            bool
	OnlyFirstPurchase       bool
	EmailDomainSuffixList   string
	EmailEnableDomainSuffix bool
	TrialEnabled            bool
	TrialSubscribeID        int64
	TrialTime               int64
	TrialTimeUnit           string
	JWTAccessSecret         string
	JWTAccessExpire         int64
}

// OAuthLoginDependencies contains the explicit collaborators of the OAuth
// login use case. It replaces the application-wide ServiceContext dependency.
type OAuthLoginDependencies struct {
	Store  OAuthLoginStore
	Redis  *redis.Client
	Config OAuthLoginConfig
	Policy OAuthRegistrationPolicy
}
