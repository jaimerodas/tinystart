package web

import (
	"context"
	"log/slog"

	"github.com/jaimerodas/tinystart/internal/postmark"
)

// LogMailer is the Mailer for a process with no Postmark token: it writes the
// message to the log instead of sending it. That is what the Rails app did in
// development with letter_opener, and it is what makes a password reset on a
// laptop — or on a server whose token has gone missing — end in a link you can
// find rather than in silence.
//
// Only the text body is logged. The HTML one says the same thing at several
// times the length, and a log line is read by a person.
type LogMailer struct {
	Logger *slog.Logger
}

func (m LogMailer) Send(_ context.Context, message postmark.Message) error {
	m.Logger.Info("mail written to the log instead of sent (no POSTMARK_API_TOKEN)",
		"to", message.To, "subject", message.Subject, "body", message.TextBody)
	return nil
}
