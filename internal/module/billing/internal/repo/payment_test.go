package repo_test

import (
	"context"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
)

// The entity refuses to delete the built-in balance method. The repository
// used to delete a zero value, so the guard never saw the method's id.
func TestPaymentRepoDeleteKeepsTheBuiltInMethod(t *testing.T) {
	h := billingtest.New(t)
	builtIn := h.Payment("balance", "", func(p *payment.Payment) { p.Id = -1 })
	other := h.Payment("EPay", `{"pid":"1001","url":"https://pay.example","key":"secret","type":"alipay"}`)
	repo := h.Store.Payment()

	if err := repo.Delete(context.Background(), builtIn.Id); err == nil {
		t.Fatal("the built-in balance method was deleted")
	}
	if _, err := repo.FindOne(context.Background(), builtIn.Id); err != nil {
		t.Fatalf("built-in method after the refused delete: %v", err)
	}
	if err := repo.Delete(context.Background(), other.Id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var remaining int64
	if err := h.DB.Model(&payment.Payment{}).Where("id = ?", other.Id).Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatalf("rows = %d (%v), want the method deleted", remaining, err)
	}
	// Deleting a method that no longer exists is not an error.
	if err := repo.Delete(context.Background(), other.Id); err != nil {
		t.Fatalf("repeated Delete: %v", err)
	}
}
