package selfsub

import (
	"context"
	"fmt"

	inboxEntity "github.com/perfect-panel/server/internal/model/entity/inbox"
	"github.com/perfect-panel/server/internal/repository"
)

// fakeInboxRepo is the shared in-memory inbox for this package's fake stores.
type fakeInboxRepo struct {
	repository.InboxRepo
	records map[string]string
}

func newFakeInboxRepo() *fakeInboxRepo {
	return &fakeInboxRepo{records: map[string]string{}}
}

func (r *fakeInboxRepo) Find(_ context.Context, consumer, key string) (*inboxEntity.Record, error) {
	result, ok := r.records[consumer+"|"+key]
	if !ok {
		return nil, nil
	}
	return &inboxEntity.Record{Consumer: consumer, EventKey: key, Result: result}, nil
}

func (r *fakeInboxRepo) Insert(_ context.Context, consumer, key, result string) error {
	k := consumer + "|" + key
	if _, ok := r.records[k]; ok {
		return fmt.Errorf("duplicate inbox record %s", k)
	}
	r.records[k] = result
	return nil
}
