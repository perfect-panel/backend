package serverapi

import (
	"context"
	"errors"
)

const (
	maxConcurrentUserListBuilds = 4
	maxQueuedUserListBuilds     = 256
)

// A gate is entered inside singleflight: each queued item represents one
// server/protocol fill, not every HTTP caller waiting for that fill. Queued
// fills hold no decoded user list. Both memory-heavy work and its backlog are
// bounded, but a routine multi-server invalidation can wait for a free worker.
type userListBuildGate struct {
	active  chan struct{}
	waiting chan struct{}
}

func newUserListBuildGate() *userListBuildGate {
	return &userListBuildGate{
		active:  make(chan struct{}, maxConcurrentUserListBuilds),
		waiting: make(chan struct{}, maxQueuedUserListBuilds),
	}
}

func (g *userListBuildGate) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return userListWaitError(err)
	}
	select {
	case g.active <- struct{}{}:
		return nil
	default:
	}
	select {
	case g.waiting <- struct{}{}:
		defer func() { <-g.waiting }()
	default:
		return ErrServerBusy
	}
	select {
	case g.active <- struct{}{}:
		if err := ctx.Err(); err != nil {
			g.release()
			return userListWaitError(err)
		}
		return nil
	case <-ctx.Done():
		return userListWaitError(ctx.Err())
	}
}

func (g *userListBuildGate) release() { <-g.active }

// An exhausted wait budget is temporary unavailability, not a missing server
// or a permission denial. Preserve context identity for callers and expose
// ErrServerBusy to HTTP so timed-out waits also carry Retry-After.
func userListWaitError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.Join(ErrServerBusy, err)
	}
	return err
}
