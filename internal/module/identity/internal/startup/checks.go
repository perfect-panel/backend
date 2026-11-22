package startup

import (
	"context"
	"sort"
	"strings"

	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthprovider"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// WarnUnpinnedOAuthRedirects logs an error when a sign-in method that sends
// the browser to a client-supplied redirect (Apple's form post, Telegram's
// widget) is enabled while no site host is configured. Their redirects are
// then pinned to the request's own Host header only, which a server exposed
// without a reverse proxy cannot trust; the operator should set the site
// host. It reports nothing when the site host is configured or no such
// method is enabled.
func (s *Service) WarnUnpinnedOAuthRedirects(ctx context.Context) error {
	if s.deps.SiteHost == nil || strings.TrimSpace(s.deps.SiteHost()) != "" || s.deps.Auths == nil {
		return nil
	}
	methods, err := s.deps.Auths.FindAll(ctx)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "list the authentication methods")
	}
	var unpinned []string
	for _, method := range methods {
		if method == nil || method.Enabled == nil || !*method.Enabled {
			continue
		}
		if spec, ok := oauthprovider.Default().Lookup(method.Method); ok && spec.PinRedirect {
			unpinned = append(unpinned, method.Method)
		}
	}
	if len(unpinned) == 0 {
		return nil
	}
	sort.Strings(unpinned)
	logger.WithContext(ctx).Errorw("[Identity] sign-in methods that redirect the browser are enabled but no site host is configured; their redirects are pinned to the request's Host header only. Set Site.Host (system settings) to pin them",
		logger.Field("methods", unpinned))
	return nil
}

// ReportLegacyAdministratorPasswords logs an error naming the ids of the
// administrators whose stored password hash is still a legacy format (md5,
// sha256, PBKDF2 or an imported bcrypt): those hashes stay accepted until
// their owner signs in, which rehashes them, so an administrator who never
// signs in keeps a weak hash. Only ids are logged, never addresses.
func (s *Service) ReportLegacyAdministratorPasswords(ctx context.Context) error {
	admins, err := s.deps.Users.QueryAdminUsers(ctx)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "query the administrators")
	}
	var legacy []int64
	for _, admin := range admins {
		if password.IsLegacyHash(admin.Password) {
			legacy = append(legacy, admin.Id)
		}
	}
	if len(legacy) == 0 {
		return nil
	}
	sort.Slice(legacy, func(i, j int) bool { return legacy[i] < legacy[j] })
	logger.WithContext(ctx).Errorw("[Security] administrators still have a legacy password hash; have them sign in or reset their password so it is rehashed",
		logger.Field("user_ids", legacy))
	return nil
}
