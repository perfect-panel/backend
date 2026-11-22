package tool

import (
	"context"
	"testing"

	"github.com/oschwald/geoip2-golang"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Without a loaded database the lookup reports an error; it used to call
// City on a nil reader and panic.
func TestQueryIPLocationWithoutDatabase(t *testing.T) {
	for name, deps := range map[string]Deps{
		"no accessor":       {},
		"database not read": {GeoIP: func() *geoip2.Reader { return nil }},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewService(deps).QueryIPLocation(context.Background(), &dto.QueryIPLocationRequest{IP: "1.1.1.1"})
			if xerr.CodeOf(err) != xerr.ERROR {
				t.Fatalf("err = %v, want an ERROR-coded failure", err)
			}
		})
	}
}

func TestQueryIPLocationRejectsInvalidAddress(t *testing.T) {
	_, err := NewService(Deps{GeoIP: func() *geoip2.Reader { return nil }}).
		QueryIPLocation(context.Background(), &dto.QueryIPLocationRequest{IP: "not-an-ip"})
	if xerr.CodeOf(err) != xerr.InvalidParams {
		t.Fatalf("err = %v, want InvalidParams", err)
	}
}
