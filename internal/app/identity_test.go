package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/module/notification"
)

// stubNotifier records the Telegram notice sent to a user; the embedded nil
// facade covers the methods the notice never calls.
type stubNotifier struct {
	notification.Service
	userID int64
	text   string
	err    error
}

func (s *stubNotifier) NotifyTelegramUser(_ context.Context, userID int64, text string) error {
	s.userID, s.text = userID, text
	return s.err
}

// The password-change notice renders as MarkdownV2 with the bindings as
// escaped data, names none when there are none, reports the bot's failure
// to the caller (who treats it as best effort) and sends nothing without a
// notifier.
func TestNotifyPasswordChangedRendersTheNotice(t *testing.T) {
	notifier := &stubNotifier{}
	if err := notifyPasswordChanged(context.Background(), notifier, 7, []string{"github", "telegram"}); err != nil {
		t.Fatalf("notifyPasswordChanged() error = %v", err)
	}
	if notifier.userID != 7 || !strings.Contains(notifier.text, "github, telegram") || !strings.Contains(notifier.text, "密码已更改") {
		t.Fatalf("notice to %d = %q", notifier.userID, notifier.text)
	}
	if err := notifyPasswordChanged(context.Background(), notifier, 7, nil); err != nil || !strings.Contains(notifier.text, "无") {
		t.Fatalf("notice without bindings = %q, %v", notifier.text, err)
	}

	notifier.err = errors.New("no telegram binding")
	if err := notifyPasswordChanged(context.Background(), notifier, 7, []string{"github"}); err == nil {
		t.Fatal("the bot's failure was not reported")
	}
	if err := notifyPasswordChanged(context.Background(), nil, 7, []string{"github"}); err != nil {
		t.Fatalf("without a notifier: %v", err)
	}
}
