package notify

import (
	"errors"
	"testing"

	"example.com/depot/retry"
)

type flakyMailer struct {
	failures int
	sent     []string
}

func (m *flakyMailer) Mail(to, body string) error {
	if m.failures > 0 {
		m.failures--
		return errors.New("smtp busy")
	}
	m.sent = append(m.sent, to+": "+body)
	return nil
}

func eventually(t *testing.T, fn func() error) {
	t.Helper()
	if err := retry.Do(9, fn); err != nil {
		t.Fatal(err)
	}
}

func TestDeliverRendersTemplate(t *testing.T) {
	mailer := &flakyMailer{failures: 1}
	sender := NewSender(mailer)
	eventually(t, func() error {
		return sender.Deliver(Message{To: "a@example.invalid", Template: "welcome", Fields: map[string]string{"name": "Ada"}})
	})
	if len(mailer.sent) != 1 || mailer.sent[0] != "a@example.invalid: Welcome, Ada." {
		t.Fatalf("sent %q", mailer.sent)
	}
}
