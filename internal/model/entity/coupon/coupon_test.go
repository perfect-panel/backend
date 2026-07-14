package coupon

import "testing"

func TestCouponIsEnabled(t *testing.T) {
	enabled := true
	disabled := false

	tests := []struct {
		name string
		item *Coupon
		want bool
	}{
		{name: "enabled", item: &Coupon{Enable: &enabled}, want: true},
		{name: "disabled", item: &Coupon{Enable: &disabled}, want: false},
		{name: "nil enable", item: &Coupon{}, want: false},
		{name: "nil coupon", item: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.item.IsEnabled(); got != tt.want {
				t.Fatalf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
