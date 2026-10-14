package notify

import (
	"errors"

	"example.com/depot/retry"
)

var ErrNoRecipient = errors.New("notify: message has no recipient")

type Mailer interface {
	Mail(to, body string) error
}

type Message struct {
	To       string
	Template string
	Fields   map[string]string
}

type Sender struct {
	mailer Mailer
	quiet  map[string]bool
}

func NewSender(m Mailer) *Sender {
	return &Sender{mailer: m, quiet: map[string]bool{}}
}

func (s *Sender) Mute(address string) {
	s.quiet[address] = true
}

func (s *Sender) Deliver(m Message) error {
	if m.To == "" {
		return ErrNoRecipient
	}
	if s.quiet[m.To] {
		return nil
	}
	body, err := render(m.Template, m.Fields)
	if err != nil {
		return err
	}
	return retry.Do(4, func() error {
		return s.mailer.Mail(m.To, body)
	})
}
