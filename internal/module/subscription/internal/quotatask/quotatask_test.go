package quotatask

import (
	"context"
	"errors"
	"testing"
	"time"

	userEntity "github.com/perfect-panel/server/internal/module/identity/entity/user"
	"github.com/perfect-panel/server/internal/module/platform/entity/inbox"
	logEntity "github.com/perfect-panel/server/internal/module/platform/entity/log"
	"github.com/perfect-panel/server/internal/module/platform/entity/task"
	"github.com/perfect-panel/server/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/server/internal/repository"
	"gorm.io/gorm"
)

type quotaFailureStore struct {
	repository.Store
	subscription repository.SubscriptionStore
}

func (s *quotaFailureStore) InSubscriptionTx(_ context.Context, fn func(repository.SubscriptionStore) error) error {
	return fn(s.subscription)
}

type quotaSubscriptionStore struct {
	repository.SubscriptionStore
	users repository.UserSubscriptionRepo
	inbox repository.InboxRepo
}

func (s *quotaSubscriptionStore) UserSubscription() repository.UserSubscriptionRepo { return s.users }
func (s *quotaSubscriptionStore) Inbox() repository.InboxRepo                       { return s.inbox }
func (s *quotaSubscriptionStore) Log() repository.LogRepo                           { return quotaLogRepo{} }

type quotaLogRepo struct{ repository.LogRepo }

func (quotaLogRepo) Insert(context.Context, *logEntity.SystemLog) error { return nil }

type failingSubscriptionRepo struct {
	repository.UserSubscriptionRepo
	err error
}

func (r *failingSubscriptionRepo) UpdateSubscribe(_ context.Context, _ *usersub.Subscribe, _ ...*gorm.DB) error {
	return r.err
}

type quotaInbox struct {
	repository.InboxRepo
	inserts int
}

type quotaGiftReplayStore struct {
	repository.Store
	inbox        repository.InboxRepo
	cache        repository.UserCacheRepo
	billingCalls int
}

func (s *quotaGiftReplayStore) Inbox() repository.InboxRepo         { return s.inbox }
func (s *quotaGiftReplayStore) UserCache() repository.UserCacheRepo { return s.cache }
func (s *quotaGiftReplayStore) InBillingTx(_ context.Context, _ func(repository.BillingStore) error) error {
	s.billingCalls++
	return errors.New("billing transaction should not run for a completed gift stage")
}

type existingQuotaInbox struct{ repository.InboxRepo }

func (existingQuotaInbox) Find(context.Context, string, string) (*inbox.Record, error) {
	return &inbox.Record{Consumer: inboxQuotaGift, EventKey: "7:9"}, nil
}

type recordingUserCache struct {
	repository.UserCacheRepo
	cleared int
}

func (c *recordingUserCache) ClearUserCache(_ context.Context, users ...*userEntity.User) error {
	c.cleared += len(users)
	return nil
}

func (r *quotaInbox) Find(context.Context, string, string) (*inbox.Record, error) { return nil, nil }
func (r *quotaInbox) Insert(context.Context, string, string, string) error {
	r.inserts++
	return nil
}

func TestGrantSubscriptionDoesNotMarkInboxAfterUpdateFailure(t *testing.T) {
	wantErr := errors.New("write failed")
	users := &failingSubscriptionRepo{err: wantErr}
	marks := &quotaInbox{}
	store := &quotaFailureStore{subscription: &quotaSubscriptionStore{users: users, inbox: marks}}
	logic := &QuotaTaskLogic{deps: Deps{Store: store}}
	sub := &usersub.Subscribe{Id: 9, ExpireTime: time.Now().Add(time.Hour)}

	err := logic.grantSubscription(context.Background(), 7, sub, task.QuotaContent{Days: 1}, time.Now())
	if err == nil || marks.inserts != 0 {
		t.Fatalf("update error must roll back without an inbox marker: err=%v inserts=%d", err, marks.inserts)
	}
}

func TestGrantSubscriptionReactivatesTrafficFinishedSubscription(t *testing.T) {
	users := &failingSubscriptionRepo{}
	marks := &quotaInbox{}
	store := &quotaFailureStore{subscription: &quotaSubscriptionStore{users: users, inbox: marks}}
	logic := &QuotaTaskLogic{deps: Deps{Store: store}}
	finishedAt := time.Now()
	sub := &usersub.Subscribe{
		Id: 9, Status: usersub.SubscribeStatusFinished, FinishedAt: &finishedAt,
		Download: 10, Upload: 20,
	}

	if err := logic.grantSubscription(context.Background(), 7, sub, task.QuotaContent{ResetTraffic: true}, time.Now()); err != nil {
		t.Fatalf("grantSubscription: %v", err)
	}
	if sub.Status != usersub.SubscribeStatusActive || sub.FinishedAt != nil || sub.Download != 0 || sub.Upload != 0 || marks.inserts != 1 {
		t.Fatalf("reset quota did not reactivate subscription atomically: sub=%+v inserts=%d", sub, marks.inserts)
	}
}

func TestGrantSubscriptionPreservesNoLimitExpiry(t *testing.T) {
	users := &failingSubscriptionRepo{}
	marks := &quotaInbox{}
	store := &quotaFailureStore{subscription: &quotaSubscriptionStore{users: users, inbox: marks}}
	logic := &QuotaTaskLogic{deps: Deps{Store: store}}
	sub := &usersub.Subscribe{Id: 9, Status: usersub.SubscribeStatusActive, ExpireTime: time.UnixMilli(0)}

	if err := logic.grantSubscription(context.Background(), 7, sub, task.QuotaContent{Days: 30}, time.Now()); err != nil {
		t.Fatalf("grantSubscription: %v", err)
	}
	if sub.ExpireTime.UnixMilli() != 0 || sub.Status != usersub.SubscribeStatusActive || marks.inserts != 1 {
		t.Fatalf("NoLimit subscription was downgraded: sub=%+v inserts=%d", sub, marks.inserts)
	}
}

func TestGrantGiftReplaySkipsPlanLookupAndRefreshesCache(t *testing.T) {
	cache := &recordingUserCache{}
	store := &quotaGiftReplayStore{inbox: existingQuotaInbox{}, cache: cache}
	logic := &QuotaTaskLogic{deps: Deps{Store: store}}

	err := logic.grantGift(context.Background(), 7, &usersub.Subscribe{Id: 9, UserId: 11, SubscribeId: 13}, task.QuotaContent{GiftType: 2, GiftValue: 10}, time.Now())
	if err != nil {
		t.Fatalf("completed gift replay: %v", err)
	}
	if store.billingCalls != 0 || cache.cleared != 1 {
		t.Fatalf("completed gift stage replayed work: billing_calls=%d cache_clears=%d", store.billingCalls, cache.cleared)
	}
}

func TestValidateContentRejectsInvalidQuotaActions(t *testing.T) {
	for _, content := range []task.QuotaContent{
		{},
		{GiftType: 1},
		{GiftType: 9, GiftValue: 1},
		{GiftType: 1, GiftValue: ^uint64(0)},
	} {
		if err := validateContent(content); err == nil {
			t.Fatalf("invalid content accepted: %+v", content)
		}
	}
	if err := validateContent(task.QuotaContent{Days: 1}); err != nil {
		t.Fatalf("valid quota action rejected: %v", err)
	}
}
