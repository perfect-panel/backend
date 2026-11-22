// Package state owns the process-wide mutable runtime state. Configuration
// is published as immutable snapshots so request and queue goroutines never
// race with an administrator-triggered reinitialization.
package state

import (
	"sync"
	"sync/atomic"

	tgbot "github.com/go-telegram/bot"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/network"
)

type restartHandler struct{ fn func() error }
type reinitializeHandler struct{ fn func(string) error }

// State is the runtime state the composition root shares with the services:
// the configuration snapshot, the Telegram bot, the node traffic multiplier
// and the late-bound restart and reinitialize handlers.
type State struct {
	configMu sync.Mutex
	config   atomic.Pointer[config.Config]

	telegramBot           atomic.Pointer[tgbot.Bot]
	nodeMultiplierManager atomic.Pointer[network.MultiplierManager]
	restart               atomic.Pointer[restartHandler]
	reinitialize          atomic.Pointer[reinitializeHandler]
}

// New returns the state holding initial as its first configuration snapshot.
func New(initial config.Config) *State {
	state := new(State)
	state.config.Store(&initial)
	return state
}

// Config returns the current immutable configuration snapshot.
func (s *State) Config() config.Config {
	if current := s.config.Load(); current != nil {
		return *current
	}
	return config.Config{}
}

// UpdateRuntime serializes partial updates of the runtime settings and
// atomically publishes the result; the boot settings cannot change while the
// process runs. Callers must replace nested slices/maps instead of mutating
// values retained from an older snapshot.
func (s *State) UpdateRuntime(update func(*config.Runtime)) {
	if update == nil {
		return
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	next := s.Config()
	update(&next.Runtime)
	s.config.Store(&next)
}

// TelegramBot returns the running bot client, nil while none is configured.
func (s *State) TelegramBot() *tgbot.Bot { return s.telegramBot.Load() }

// SetTelegramBot publishes the bot client the bootstrap (re)initialized.
func (s *State) SetTelegramBot(bot *tgbot.Bot) { s.telegramBot.Store(bot) }

// NodeMultiplierManager returns the node traffic multiplier periods, nil
// before the node settings were loaded.
func (s *State) NodeMultiplierManager() *network.MultiplierManager {
	return s.nodeMultiplierManager.Load()
}

// SetNodeMultiplierManager publishes the multiplier periods the node
// settings define.
func (s *State) SetNodeMultiplierManager(manager *network.MultiplierManager) {
	s.nodeMultiplierManager.Store(manager)
}

// SetRestart installs the HTTP server's restart; the server installs it once
// it started.
func (s *State) SetRestart(handler func() error) {
	if handler != nil {
		s.restart.Store(&restartHandler{fn: handler})
	}
}

// Restart restarts the HTTP server; before the server installed its handler
// there is nothing to restart.
func (s *State) Restart() error {
	if handler := s.restart.Load(); handler != nil {
		return handler.fn()
	}
	return nil
}

// SetReinitialize installs the subsystem reload the HTTP server runs the
// bootstrap with.
func (s *State) SetReinitialize(handler func(string) error) {
	if handler != nil {
		s.reinitialize.Store(&reinitializeHandler{fn: handler})
	}
}

// Reinitialize reloads a subsystem's runtime configuration and reports a
// failure; before the server installed its handler there is nothing to
// reload.
func (s *State) Reinitialize(subsystem string) error {
	if handler := s.reinitialize.Load(); handler != nil {
		return handler.fn(subsystem)
	}
	return nil
}
