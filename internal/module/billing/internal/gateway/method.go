package gateway

import (
	"context"
	"errors"
	"strings"

	paymentEntity "github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// MethodFinder loads a payment method by id.
type MethodFinder interface {
	FindOne(ctx context.Context, id int64) (*paymentEntity.Payment, error)
}

// LookupMethod loads the payment method an order is to be paid with. Every
// order flow maps its failures the same way: a method that does not exist,
// is disabled or belongs to an unsupported platform is PaymentMethodNotFound,
// and a failed lookup is a database error.
func LookupMethod(ctx context.Context, methods MethodFinder, id int64) (*paymentEntity.Payment, error) {
	method, err := methods.FindOne(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, xerr.Errorf(xerr.PaymentMethodNotFound, "payment method %d does not exist", id)
	}
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find payment method %d", id)
	}
	if err := EnsureAvailable(method); err != nil {
		return nil, err
	}
	return method, nil
}

// EnsureAvailable rejects a payment method a buyer may not use.
func EnsureAvailable(method *paymentEntity.Payment) error {
	if method == nil || method.Enable == nil || !*method.Enable || payment.ParsePlatform(method.Platform) == payment.UNSUPPORTED {
		return xerr.Errorf(xerr.PaymentMethodNotFound, "payment method is unavailable")
	}
	return nil
}

// IsBalance reports whether the method is the internal wallet balance.
func IsBalance(method *paymentEntity.Payment) bool {
	return method != nil && payment.ParsePlatform(method.Platform) == payment.Balance
}

// Rates prices currencies against each other.
type Rates interface {
	// Rate is the price of one unit of from in units of to.
	Rate(ctx context.Context, from, to string) (float64, error)
}

// ChargeFor returns what gw collects for amount minor units of the system
// currency: the amount itself when the gateway collects the system currency,
// otherwise its conversion into the gateway's currency at the current rate
// (see payment.ConvertAmount for the rounding).
func ChargeFor(ctx context.Context, gw Gateway, amount int64, systemCurrency string, rates Rates) (Charge, error) {
	charge, err := chargeFor(ctx, gw, amount, systemCurrency, rates)
	if err != nil {
		return Charge{}, err
	}
	if rounder, ok := gw.(ChargeRounder); ok {
		charge = rounder.RoundCharge(charge)
	}
	return charge, nil
}

// ChargeRounder is a gateway that collects some currencies at a coarser
// precision than hundredths (Stripe in JPY collects whole yen). ChargeFor
// rounds the charge to it before the expectation is recorded, so the amount
// the gateway reports back matches the expectation exactly.
type ChargeRounder interface {
	RoundCharge(Charge) Charge
}

func chargeFor(ctx context.Context, gw Gateway, amount int64, systemCurrency string, rates Rates) (Charge, error) {
	system := strings.ToUpper(strings.TrimSpace(systemCurrency))
	target := strings.ToUpper(gw.ChargeCurrency())
	if target == "" || target == system {
		return Charge{Amount: amount, Currency: system}, nil
	}
	if rates == nil {
		return Charge{}, errors.New("exchange rate is not configured")
	}
	rate, err := rates.Rate(ctx, system, target)
	if err != nil {
		return Charge{}, err
	}
	converted, err := payment.ConvertAmount(amount, rate)
	if err != nil {
		return Charge{}, err
	}
	return Charge{Amount: converted, Currency: target}, nil
}
