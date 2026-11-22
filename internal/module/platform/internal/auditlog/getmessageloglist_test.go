package auditlog

import (
	"context"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
)

func TestGetMessageLogListRejectsNonMessageTypes(t *testing.T) {
	svc := NewService(Deps{})
	for _, typ := range []uint8{0, 20, 30, 33, 42} {
		if _, err := svc.GetMessageLogList(context.Background(), &dto.GetMessageLogListRequest{Page: 1, Size: 10, Type: typ}); err == nil {
			t.Fatalf("type %d was accepted", typ)
		}
	}
}
