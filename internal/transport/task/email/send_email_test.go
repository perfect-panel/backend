package email

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
)

// The queued literal is only the fallback: an operator-configured subject
// wins and renders against the same data as the body.
func TestResolveSubjectPrefersConfiguredTemplate(t *testing.T) {
	data := map[string]any{"SiteName": "示例站"}

	got := resolveSubject(context.Background(), "【{{.SiteName}}】订阅已到期", "Subscription Expired", data)
	if got != "【示例站】订阅已到期" {
		t.Fatalf("subject = %q, want the rendered configured template", got)
	}
}

func TestResolveSubjectFallsBackWhenUnconfigured(t *testing.T) {
	got := resolveSubject(context.Background(), "", "Subscription Expired", nil)
	if got != "Subscription Expired" {
		t.Fatalf("subject = %q, want the queued fallback", got)
	}
}

// A subject with a template typo is still sent as the operator wrote it; the
// localized text beats silently reverting to English.
func TestResolveSubjectKeepsRawTextOnBadTemplate(t *testing.T) {
	got := resolveSubject(context.Background(), "订阅已到期 {{.SiteName", "Subscription Expired", nil)
	if got != "订阅已到期 {{.SiteName" {
		t.Fatalf("subject = %q, want the raw configured text", got)
	}
}

func TestRenderEmailTemplateReportsParseErrors(t *testing.T) {
	if _, err := renderEmailTemplate("body", "{{.Broken", nil); err == nil {
		t.Fatal("parse error was swallowed")
	}
	rendered, err := renderEmailTemplate("body", "Hello {{.Name}}", map[string]any{"Name": "User"})
	if err != nil || rendered != "Hello User" {
		t.Fatalf("rendered = %q, err = %v", rendered, err)
	}
}

func TestEmailLogContentRedactsVerificationCode(t *testing.T) {
	content := map[string]any{"Code": "123456", "SiteName": "Example"}

	redacted := emailLogContent(taskqueue.EmailTypeVerify, content)
	if redacted["redacted"] != true {
		t.Fatalf("verification log content = %#v", redacted)
	}
	if _, ok := redacted["Code"]; ok {
		t.Fatalf("verification log contains code: %#v", redacted)
	}
	if content["Code"] != "123456" {
		t.Fatalf("rendering content was mutated: %#v", content)
	}
}

func TestEmailLogContentRedactsNonVerificationContent(t *testing.T) {
	content := map[string]any{"message": "maintenance"}

	if got := emailLogContent(taskqueue.EmailTypeMaintenance, content); got["redacted"] != true || got["email_type"] != taskqueue.EmailTypeMaintenance {
		t.Fatalf("non-verification log content = %#v", got)
	}
}

// A verification payload without its code purpose is dropped: the unchecked
// conversion it used to go through panicked, and the task was retried until
// asynq gave up.
func TestProcessTaskDropsAVerificationWithoutItsPurpose(t *testing.T) {
	handler := NewSendEmailHandler(Dependencies{
		Email: func() config.EmailConfig {
			return config.EmailConfig{Platform: "smtp", PlatformConfig: `{"host":"127.0.0.1","port":2525,"from":"noreply@example.com"}`}
		},
		SiteName: func() string { return "Perfect Panel" },
	})
	for name, content := range map[string]map[string]any{
		"no purpose":        {"Code": "123456"},
		"purpose as string": {"Type": "1", "Code": "123456"},
		"no content":        nil,
	} {
		t.Run(name, func(t *testing.T) {
			payload, err := json.Marshal(taskqueue.SendEmailPayload{Type: taskqueue.EmailTypeVerify, Email: "user@example.com", Content: content})
			if err != nil {
				t.Fatal(err)
			}
			// Logs is nil: a message log written for the payload would panic.
			if err := handler.ProcessTask(context.Background(), asynq.NewTask(taskqueue.ForthwithSendEmail, payload)); err != nil {
				t.Fatalf("ProcessTask = %v, want the payload dropped", err)
			}
		})
	}
}
