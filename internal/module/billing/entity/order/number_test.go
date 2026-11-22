package order

import (
	"testing"
	"time"

	"github.com/perfect-panel/backend/pkg/timeutil"
)

func TestGenerateTradeNoFormat(t *testing.T) {
	no := GenerateTradeNo()
	if len(no) != 14+tradeNoRandomDigits {
		t.Fatalf("trade number %q has length %d, want fixed %d", no, len(no), 14+tradeNoRandomDigits)
	}
	for i, c := range no {
		if c < '0' || c > '9' {
			t.Fatalf("trade number %q has non-digit %q at %d", no, c, i)
		}
	}
	if stamp, err := time.ParseInLocation("20060102150405", no[:14], timeutil.Location()); err != nil {
		t.Fatalf("trade number %q does not start with a timestamp: %v", no, err)
	} else if time.Since(stamp) > time.Minute {
		t.Fatalf("trade number timestamp %v is not recent", stamp)
	}
}

func TestGenerateTradeNoUnique(t *testing.T) {
	const n = 10000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		no := GenerateTradeNo()
		if _, dup := seen[no]; dup {
			t.Fatalf("duplicate trade number %q after %d generations", no, i)
		}
		seen[no] = struct{}{}
	}
}
