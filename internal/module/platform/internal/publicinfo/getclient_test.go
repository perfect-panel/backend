package publicinfo

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// clientApplications is the subscription module's client list as the
// composition root hands it over.
type clientApplications struct {
	apps []ClientApplication
	err  error
}

var _ ClientApplicationLister = clientApplications{}

func (c clientApplications) ListClientApplications(context.Context) ([]ClientApplication, error) {
	return c.apps, c.err
}

// The download page lists the client applications in their stored order
// with their decoded download links.
func TestGetClientListsTheClientApplications(t *testing.T) {
	svc := NewService(Deps{Clients: clientApplications{apps: []ClientApplication{
		{Id: 1, Name: "Clash", Description: "rules", Icon: "icon", Scheme: "clash://", IsDefault: true, DownloadLink: `{"ios":"https://example.com/ios"}`},
		{Id: 2, Name: "Plain"},
	}}})
	resp, err := svc.GetClient(context.Background())
	if err != nil {
		t.Fatalf("GetClient() error = %v", err)
	}
	if resp.Total != 2 || len(resp.List) != 2 {
		t.Fatalf("response = %+v", resp)
	}
	first, second := resp.List[0], resp.List[1]
	if first.Id != 1 || first.Name != "Clash" || first.Description != "rules" || first.Icon != "icon" ||
		first.Scheme != "clash://" || !first.IsDefault || first.DownloadLink.IOS != "https://example.com/ios" {
		t.Fatalf("first client = %+v", first)
	}
	if second.Id != 2 || second.IsDefault || second.DownloadLink.IOS != "" {
		t.Fatalf("second client = %+v", second)
	}
}

// A failed read is a database query error.
func TestGetClientReportsAFailedRead(t *testing.T) {
	logtest.Discard(t)
	svc := NewService(Deps{Clients: clientApplications{err: errors.New("database unavailable")}})
	_, err := svc.GetClient(context.Background())
	if got := xerr.CodeOf(err); err == nil || got != xerr.DatabaseQueryError {
		t.Fatalf("error = %v (code %d), want %d", err, got, xerr.DatabaseQueryError)
	}
}
