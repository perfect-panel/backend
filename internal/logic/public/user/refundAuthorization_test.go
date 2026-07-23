package user

import (
	"context"
	"testing"

	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	usermodel "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger/logtest"
	"github.com/perfect-panel/server/pkg/xerr"
)

// fakeUserRepo embeds repository.UserRepo (nil) so any unexpected
// method call panics immediately (fail-fast).
type fakeUserRepo struct {
	repository.UserRepo
	repository.UserSubscriptionRepo

	findOneSubscribeFn    func(context.Context, int64) (*usermodel.Subscribe, error)
	findOneSubscribeCalls int

	findOneUserSubscribeFn    func(context.Context, int64) (*usermodel.SubscribeDetails, error)
	findOneUserSubscribeCalls int
}

func (r *fakeUserRepo) FindOneSubscribe(ctx context.Context, id int64) (*usermodel.Subscribe, error) {
	r.findOneSubscribeCalls++
	if r.findOneSubscribeFn != nil {
		return r.findOneSubscribeFn(ctx, id)
	}
	panic("fakeUserRepo: unexpected call to FindOneSubscribe")
}

func (r *fakeUserRepo) FindOneUserSubscribe(ctx context.Context, id int64) (*usermodel.SubscribeDetails, error) {
	r.findOneUserSubscribeCalls++
	if r.findOneUserSubscribeFn != nil {
		return r.findOneUserSubscribeFn(ctx, id)
	}
	panic("fakeUserRepo: unexpected call to FindOneUserSubscribe")
}

// fakeStore embeds repository.Store (nil) so any unexpected method
// call panics immediately.
type fakeStore struct {
	repository.Store
	uRepo *fakeUserRepo
}

func (s *fakeStore) User() repository.UserRepo { return s.uRepo }
func (s *fakeStore) UserSubscription() repository.UserSubscriptionRepo {
	return s.uRepo
}

func newFakeSvcCtx(uRepo *fakeUserRepo) *svc.ServiceContext {
	return &svc.ServiceContext{
		Store:  &fakeStore{uRepo: uRepo},
		Config: config.Config{},
	}
}

// errCode extracts the xerr.CodeError from the wrapped error chain.
func errCode(t *testing.T, err error) uint32 {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *xerr.CodeError
	if !errors.As(errors.Cause(err), &ce) {
		t.Fatalf("expected *xerr.CodeError in chain, got %T", err)
	}
	return ce.GetErrCode()
}

// ---------------------------------------------------------------------------
// PreUnsubscribe – authorization-gate tests
// ---------------------------------------------------------------------------

func TestPreUnsubscribe_WrongOwner_ReturnsInvalidAccess(t *testing.T) {
	logtest.Discard(t)

	ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &usermodel.User{Id: 100})
	const subID int64 = 200

	u := &fakeUserRepo{
		findOneSubscribeFn: func(_ context.Context, id int64) (*usermodel.Subscribe, error) {
			if id != subID {
				t.Fatalf("FindOneSubscribe: got id %d, want %d", id, subID)
			}
			return &usermodel.Subscribe{Id: subID, UserId: 200}, nil
		},
	}

	logic := NewPreUnsubscribeLogic(ctx, newFakeSvcCtx(u))
	resp, err := logic.PreUnsubscribe(&dto.PreUnsubscribeRequest{Id: subID})

	if code := errCode(t, err); code != xerr.InvalidAccess {
		t.Fatalf("code = %d, want %d (InvalidAccess)", code, xerr.InvalidAccess)
	}
	if resp != nil {
		t.Fatalf("resp = %+v, want nil", resp)
	}
	if u.findOneSubscribeCalls != 1 {
		t.Fatalf("FindOneSubscribe called %d time(s), want 1", u.findOneSubscribeCalls)
	}
	if u.findOneUserSubscribeCalls != 0 {
		t.Fatalf("FindOneUserSubscribe called %d time(s), want 0", u.findOneUserSubscribeCalls)
	}
}

func TestPreUnsubscribe_OwnerBypassesAuthGate(t *testing.T) {
	logtest.Discard(t)

	ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &usermodel.User{Id: 100})
	const subID int64 = 100

	u := &fakeUserRepo{
		findOneSubscribeFn: func(_ context.Context, id int64) (*usermodel.Subscribe, error) {
			if id != subID {
				t.Fatalf("FindOneSubscribe: got id %d, want %d", id, subID)
			}
			return &usermodel.Subscribe{Id: subID, UserId: 100}, nil
		},
		findOneUserSubscribeFn: func(_ context.Context, id int64) (*usermodel.SubscribeDetails, error) {
			return nil, errors.New("simulated FindOneUserSubscribe failure")
		},
	}

	logic := NewPreUnsubscribeLogic(ctx, newFakeSvcCtx(u))
	resp, err := logic.PreUnsubscribe(&dto.PreUnsubscribeRequest{Id: subID})

	if code := errCode(t, err); code == xerr.InvalidAccess {
		t.Fatal("got InvalidAccess – auth gate should not have blocked the owner")
	}
	if resp != nil {
		t.Fatalf("resp = %+v, want nil (expected downstream error)", resp)
	}
	if u.findOneSubscribeCalls != 1 {
		t.Fatalf("FindOneSubscribe called %d time(s), want 1", u.findOneSubscribeCalls)
	}
	if u.findOneUserSubscribeCalls != 1 {
		t.Fatalf("FindOneUserSubscribe called %d time(s), want 1", u.findOneUserSubscribeCalls)
	}
}

// ---------------------------------------------------------------------------
// Unsubscribe – authorization-gate tests
// ---------------------------------------------------------------------------

func TestUnsubscribe_WrongOwner_ReturnsInvalidAccess(t *testing.T) {
	logtest.Discard(t)

	ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &usermodel.User{Id: 100})
	const subID int64 = 200

	u := &fakeUserRepo{
		findOneSubscribeFn: func(_ context.Context, id int64) (*usermodel.Subscribe, error) {
			if id != subID {
				t.Fatalf("FindOneSubscribe: got id %d, want %d", id, subID)
			}
			return &usermodel.Subscribe{Id: subID, UserId: 200, Status: 1}, nil
		},
	}

	logic := NewUnsubscribeLogic(ctx, newFakeSvcCtx(u))
	err := logic.Unsubscribe(&dto.UnsubscribeRequest{Id: subID})

	if code := errCode(t, err); code != xerr.InvalidAccess {
		t.Fatalf("code = %d, want %d (InvalidAccess)", code, xerr.InvalidAccess)
	}
	if u.findOneSubscribeCalls != 1 {
		t.Fatalf("FindOneSubscribe called %d time(s), want 1", u.findOneSubscribeCalls)
	}
	if u.findOneUserSubscribeCalls != 0 {
		t.Fatalf("FindOneUserSubscribe called %d time(s), want 0", u.findOneUserSubscribeCalls)
	}
}

func TestUnsubscribe_OwnerBypassesAuthGate(t *testing.T) {
	logtest.Discard(t)

	ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &usermodel.User{Id: 100})
	const subID int64 = 100

	u := &fakeUserRepo{
		findOneSubscribeFn: func(_ context.Context, id int64) (*usermodel.Subscribe, error) {
			if id != subID {
				t.Fatalf("FindOneSubscribe: got id %d, want %d", id, subID)
			}
			return &usermodel.Subscribe{Id: subID, UserId: 100, Status: 1}, nil
		},
		findOneUserSubscribeFn: func(_ context.Context, id int64) (*usermodel.SubscribeDetails, error) {
			return nil, errors.New("simulated FindOneUserSubscribe failure")
		},
	}

	logic := NewUnsubscribeLogic(ctx, newFakeSvcCtx(u))
	err := logic.Unsubscribe(&dto.UnsubscribeRequest{Id: subID})

	if code := errCode(t, err); code == xerr.InvalidAccess {
		t.Fatal("got InvalidAccess – auth gate should not have blocked the owner")
	}
	if u.findOneSubscribeCalls != 1 {
		t.Fatalf("FindOneSubscribe called %d time(s), want 1", u.findOneSubscribeCalls)
	}
	if u.findOneUserSubscribeCalls != 1 {
		t.Fatalf("FindOneUserSubscribe called %d time(s), want 1", u.findOneUserSubscribeCalls)
	}
}
