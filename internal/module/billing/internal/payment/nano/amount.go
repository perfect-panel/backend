package nano

import (
	"errors"
	"math/big"
	"strings"
)

// RawDecimals is the number of decimal places between one XNO and one raw:
// 1 XNO = 10^30 raw.
const RawDecimals = 30

// ErrInvalidAmount is returned for an amount that is not a positive decimal
// with at most RawDecimals fractional digits, or that does not convert
// exactly.
var ErrInvalidAmount = errors.New("nano: invalid XNO amount")

// minorPerUnit is how many minor units one currency unit holds; orders count
// hundredths, as payment.FormatAmount does.
var minorPerUnit = big.NewInt(100)

// RawFromXNO converts a decimal XNO string such as "0.001" to raw using
// integer arithmetic only. Floats, signs, exponents and more than 30
// fractional digits are refused.
func RawFromXNO(xno string) (*big.Int, error) {
	s := strings.TrimSpace(xno)
	if s == "" {
		return nil, ErrInvalidAmount
	}
	whole, frac, hasDot := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	if hasDot && frac == "" {
		return nil, ErrInvalidAmount
	}
	if len(frac) > RawDecimals || !allDigits(whole) || !allDigits(frac) {
		return nil, ErrInvalidAmount
	}
	digits := whole + frac + strings.Repeat("0", RawDecimals-len(frac))
	raw, ok := new(big.Int).SetString(digits, 10)
	if !ok || raw.Sign() <= 0 {
		return nil, ErrInvalidAmount
	}
	return raw, nil
}

// ParseRaw parses a raw amount as a node returns it (a base-10 integer
// string). It refuses anything that is not a non-negative integer.
func ParseRaw(raw string) (*big.Int, error) {
	if raw == "" || len(raw) > 40 || !allDigits(raw) {
		return nil, ErrInvalidAmount
	}
	v, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return nil, ErrInvalidAmount
	}
	return v, nil
}

// Rate is the operator's price of one currency unit in raw. It is built from
// a decimal XNO string and must be a whole number of raw per minor unit, so
// every order amount converts to raw and back without rounding.
type Rate struct {
	rawPerUnit *big.Int
}

// ParseRate reads the XNO price of one currency unit, e.g. "2.5".
func ParseRate(xnoPerUnit string) (Rate, error) {
	raw, err := RawFromXNO(xnoPerUnit)
	if err != nil {
		return Rate{}, err
	}
	if new(big.Int).Rem(raw, minorPerUnit).Sign() != 0 {
		return Rate{}, ErrInvalidAmount
	}
	return Rate{rawPerUnit: raw}, nil
}

// RawPerUnit returns the raw price of one currency unit.
func (r Rate) RawPerUnit() *big.Int {
	if r.rawPerUnit == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(r.rawPerUnit)
}

// Raw converts a positive amount in minor units to raw.
func (r Rate) Raw(minor int64) (*big.Int, error) {
	if r.rawPerUnit == nil || minor <= 0 {
		return nil, ErrInvalidAmount
	}
	perMinor := new(big.Int).Quo(r.rawPerUnit, minorPerUnit)
	return perMinor.Mul(perMinor, big.NewInt(minor)), nil
}

// Minor converts raw back to minor units. A raw amount that is not an exact
// number of minor units is refused: it is not the price of any order.
func (r Rate) Minor(raw *big.Int) (int64, error) {
	if r.rawPerUnit == nil || raw == nil || raw.Sign() <= 0 {
		return 0, ErrInvalidAmount
	}
	perMinor := new(big.Int).Quo(r.rawPerUnit, minorPerUnit)
	minor, rem := new(big.Int).QuoRem(raw, perMinor, new(big.Int))
	if rem.Sign() != 0 || !minor.IsInt64() {
		return 0, ErrInvalidAmount
	}
	return minor.Int64(), nil
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
