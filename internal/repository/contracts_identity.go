package repository

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
)

// AuthRepo manages the auth_method rows: the settings of each sign-in method
// (email, mobile, device and the OAuth providers) administrators configure.
type AuthRepo interface {
	Insert(ctx context.Context, data *auth.Auth) error
	FindOne(ctx context.Context, id int64) (*auth.Auth, error)
	Update(ctx context.Context, data *auth.Auth) error
	Delete(ctx context.Context, id int64) error
	GetAuthListByPage(ctx context.Context) ([]*auth.Auth, error)
	FindOneByMethod(ctx context.Context, platform string) (*auth.Auth, error)
	FindAll(ctx context.Context) ([]*auth.Auth, error)
}
