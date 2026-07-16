package payment

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// ParseAmount converts a non-negative decimal currency amount to its integer
// minor unit. Financial callback code must not compare parsed float64 values.
func ParseAmount(value string) (int64, error) {
	if value == "" || len(value) > 20 || strings.TrimSpace(value) != value {
		return 0, errors.New("invalid money format")
	}
	wholePart, fractionalPart, hasFraction := strings.Cut(value, ".")
	if wholePart == "" || !decimalDigits(wholePart) {
		return 0, errors.New("invalid money format")
	}
	if hasFraction {
		if fractionalPart == "" || len(fractionalPart) > 2 || !decimalDigits(fractionalPart) {
			return 0, errors.New("invalid money format")
		}
	}
	whole, err := strconv.ParseInt(wholePart, 10, 64)
	if err != nil || whole > math.MaxInt64/100 {
		return 0, errors.New("money amount out of range")
	}
	fraction := int64(0)
	if hasFraction {
		if len(fractionalPart) == 1 {
			fractionalPart += "0"
		}
		fraction, err = strconv.ParseInt(fractionalPart, 10, 64)
		if err != nil {
			return 0, errors.New("invalid money format")
		}
	}
	minorUnits := whole * 100
	if fraction > math.MaxInt64-minorUnits {
		return 0, errors.New("money amount out of range")
	}
	return minorUnits + fraction, nil
}

func decimalDigits(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
