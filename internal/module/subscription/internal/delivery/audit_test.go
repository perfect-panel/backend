package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/requestmeta"
)

// deliveryAuditRepo records the audit row, or fails with err.
type deliveryAuditRepo struct {
	row *log.SystemLog
	err error
}

var _ AuditLog = (*deliveryAuditRepo)(nil)

func (r *deliveryAuditRepo) Insert(_ context.Context, row *log.SystemLog) error {
	if r.err != nil {
		return r.err
	}
	copy := *row
	r.row = &copy
	return nil
}

func TestSubscriptionAuditIsRedactedAndFailClosed(t *testing.T) {
	repo := &deliveryAuditRepo{}
	svc := NewService(Deps{Logs: repo})
	meta := RequestMeta{ClientIP: "192.0.2.1", UserAgent: "risk-client/1.0"}
	sub := &usersub.Subscribe{Id: 9, UserId: 7}

	if err := svc.logSubscribeActivity(context.Background(), meta, sub); err != nil {
		t.Fatal(err)
	}
	if repo.row == nil || strings.Contains(repo.row.Content, "subscription-secret") || !strings.Contains(repo.row.Content, logger.RedactedValue) || !strings.Contains(repo.row.Content, "192.0.2.1") || !strings.Contains(repo.row.Content, "risk-client/1.0") {
		t.Fatalf("unsafe subscription audit: %+v", repo.row)
	}

	repo.err = errors.New("audit unavailable")
	if err := svc.logSubscribeActivity(context.Background(), meta, sub); err == nil {
		t.Fatal("subscription audit failure was swallowed")
	}
}

// The row is complete as encoded: the request's IP metadata is in it, so the
// audit store has nothing left to merge.
func TestSubscriptionAuditCarriesTheRequestMetadata(t *testing.T) {
	repo := &deliveryAuditRepo{}
	ctx := requestmeta.With(context.Background(), requestmeta.Metadata{
		ClientIP: "192.0.2.1", UserAgent: "risk-client/1.0",
		IPMetadata: requestmeta.IPMetadata{IPCountryCode: "NL", IPCity: "Amsterdam", IPASN: 64500},
	})
	svc := NewService(Deps{Logs: repo})
	if err := svc.logSubscribeActivity(ctx, RequestMeta{ClientIP: "192.0.2.1", UserAgent: "risk-client/1.0"}, &usersub.Subscribe{Id: 9, UserId: 7}); err != nil {
		t.Fatal(err)
	}
	var content log.Subscribe
	if err := content.Unmarshal([]byte(repo.row.Content)); err != nil {
		t.Fatal(err)
	}
	if content.IPCountryCode != "NL" || content.IPCity != "Amsterdam" || content.IPASN != 64500 || content.UserSubscribeId != 9 {
		t.Fatalf("audit row lacks the request metadata: %+v", content)
	}
}
