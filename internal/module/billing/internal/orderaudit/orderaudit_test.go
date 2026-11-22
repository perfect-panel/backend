package orderaudit

import (
	"context"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	logEntity "github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/requestmeta"
)

// captureLogRepo keeps the last entry written to the audit log.
type captureLogRepo struct {
	entry *logEntity.SystemLog
}

var _ LogWriter = (*captureLogRepo)(nil)

func (r *captureLogRepo) Insert(_ context.Context, entry *logEntity.SystemLog) error {
	r.entry = entry
	return nil
}

func TestInsertCreatedKeepsRiskMetadataAndExcludesSecrets(t *testing.T) {
	repo := &captureLogRepo{}
	ctx := requestmeta.With(context.Background(), requestmeta.Metadata{
		ClientIP:  "203.0.113.8",
		UserAgent: "checkout-agent",
		ActorID:   42,
		IPMetadata: requestmeta.IPMetadata{
			IPCountryCode:    "SG",
			IPASN:            64500,
			IPASOrganization: "Example Network",
		},
	})
	data := &order.Order{
		UserId:            42,
		OrderNo:           "202608290002",
		Type:              1,
		Quantity:          3,
		Price:             12000,
		Amount:            9900,
		Coupon:            "PRIVATE-COUPON",
		TradeNo:           "PRIVATE-TRADE-NO",
		GuestIdentifier:   "person@example.com",
		GuestPasswordHash: "PRIVATE-PASSWORD-HASH",
		IdempotencyKey:    "PRIVATE-IDEMPOTENCY-KEY",
		PaymentId:         9,
		Method:            "stripe",
		SubscribeId:       7,
	}

	if err := InsertCreated(ctx, repo, data, SourceUser); err != nil {
		t.Fatal(err)
	}
	if repo.entry == nil {
		t.Fatal("order audit log was not inserted")
	}
	if repo.entry.Type != logEntity.TypeOrderCreated.Uint8() || repo.entry.ObjectID != data.UserId {
		t.Fatalf("unexpected audit envelope: %+v", repo.entry)
	}

	content := repo.entry.Content
	for _, expected := range []string{"202608290002", "203.0.113.8", "checkout-agent", `"actor_id":42`, `"ip_country_code":"SG"`, `"ip_asn":64500`, `"source":"user"`} {
		if !strings.Contains(content, expected) {
			t.Errorf("order audit lost %q: %s", expected, content)
		}
	}
	for _, secret := range []string{"PRIVATE-COUPON", "PRIVATE-TRADE-NO", "person@example.com", "PRIVATE-PASSWORD-HASH", "PRIVATE-IDEMPOTENCY-KEY"} {
		if strings.Contains(content, secret) {
			t.Errorf("order audit contains secret %q: %s", secret, content)
		}
	}
}
