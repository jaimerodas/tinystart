package web

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/jaimerodas/tinystart/internal/postmark"
)

// Without a Postmark token there is nobody to send through, and the app on a
// laptop still has to be able to say where a reset link went — which is what
// letter_opener did for the Rails app. The log is where it goes.
func TestLogMailerWritesTheMessageToTheLog(t *testing.T) {
	var out bytes.Buffer
	mailer := LogMailer{Logger: slog.New(slog.NewTextHandler(&out, nil))}

	err := mailer.Send(context.Background(), postmark.Message{
		To:       "jaime@example.com",
		Subject:  "Reset your password",
		TextBody: "Visit https://example.com/passwords/abc/edit to reset it.",
		HTMLBody: "<p>ignored here</p>",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	for _, want := range []string{"jaime@example.com", "Reset your password", "https://example.com/passwords/abc/edit"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("log = %q, want it to contain %q", out.String(), want)
		}
	}
	if strings.Contains(out.String(), "<p>") {
		t.Error("the HTML body was logged; the text body is the readable one")
	}
}
