package state

import (
	"errors"
	"sync"
	"testing"

	"github.com/perfect-panel/backend/internal/config"
)

func TestStatePublishesConcurrentConfigUpdates(t *testing.T) {
	var initial config.Config
	initial.Port = 8080
	state := New(initial)
	const updates = 64

	var writers sync.WaitGroup
	for i := 0; i < updates; i++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			state.UpdateRuntime(func(current *config.Runtime) {
				current.Node.NodePullInterval++
			})
		}()
	}

	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 16; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = state.Config().Node.NodePullInterval
				}
			}
		}()
	}

	writers.Wait()
	close(stop)
	readers.Wait()
	if got := state.Config().Node.NodePullInterval; got != updates {
		t.Fatalf("lost concurrent updates: got %d, want %d", got, updates)
	}
	// Runtime updates leave the boot settings alone.
	if got := state.Config().Port; got != 8080 {
		t.Fatalf("port = %d after runtime updates, want 8080", got)
	}
}

func TestStateLifecycleHandlers(t *testing.T) {
	state := New(config.Config{})
	if err := state.Restart(); err != nil {
		t.Fatalf("unexpected unavailable restart result: %v", err)
	}

	restarted := false
	state.SetRestart(func() error { restarted = true; return nil })
	if err := state.Restart(); err != nil || !restarted {
		t.Fatalf("restart handler was not invoked: restarted=%v err=%v", restarted, err)
	}

	if err := state.Reinitialize("node"); err != nil {
		t.Fatalf("reinitialize without a handler = %v, want nothing to reload", err)
	}
	var subsystem string
	failure := errors.New("reload failed")
	state.SetReinitialize(func(value string) error { subsystem = value; return failure })
	if err := state.Reinitialize("node"); !errors.Is(err, failure) || subsystem != "node" {
		t.Fatalf("reinitialize = %v for %q, want the handler's failure for node", err, subsystem)
	}
}
