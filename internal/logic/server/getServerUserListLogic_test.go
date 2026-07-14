package server

import (
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/perfect-panel/server/internal/model/entity/subscribe"
)

func TestPlaceholderServerUserIsStable(t *testing.T) {
	first := placeholderServerUser(1, "vless", "secret")
	second := placeholderServerUser(1, "vless", "secret")
	otherProtocol := placeholderServerUser(1, "trojan", "secret")
	otherSecret := placeholderServerUser(1, "vless", "other-secret")

	if first.Id != 1 {
		t.Fatalf("placeholder user id = %d, want 1", first.Id)
	}
	if first.UUID == "" {
		t.Fatal("placeholder uuid is empty")
	}
	if _, err := uuid.FromString(first.UUID); err != nil {
		t.Fatalf("placeholder uuid is invalid: %v", err)
	}
	if first.UUID != second.UUID {
		t.Fatalf("placeholder uuid changed for same input: %s != %s", first.UUID, second.UUID)
	}
	if first.UUID == otherProtocol.UUID {
		t.Fatalf("placeholder uuid should include protocol: %s", first.UUID)
	}
	if first.UUID == otherSecret.UUID {
		t.Fatalf("placeholder uuid should include node secret: %s", first.UUID)
	}
}

func TestMergeSubscribeLists(t *testing.T) {
	sub1 := &subscribe.Subscribe{Id: 1}
	sub2 := &subscribe.Subscribe{Id: 2}
	sub3 := &subscribe.Subscribe{Id: 3}

	got := mergeSubscribeLists(
		[]*subscribe.Subscribe{sub1, sub2, nil},
		[]*subscribe.Subscribe{sub2, sub3},
		[]*subscribe.Subscribe{sub1},
	)

	if len(got) != 3 {
		t.Fatalf("mergeSubscribeLists() len = %d, want 3", len(got))
	}
	for i, wantID := range []int64{1, 2, 3} {
		if got[i].Id != wantID {
			t.Fatalf("mergeSubscribeLists()[%d].Id = %d, want %d", i, got[i].Id, wantID)
		}
	}
}
