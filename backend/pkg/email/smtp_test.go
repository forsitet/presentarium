package email_test

import (
	"strings"
	"testing"

	"presentarium/pkg/email"
)

func TestNewSender_EmptyHostReturnsNil(t *testing.T) {
	if s := email.NewSender("", 587, "u", "p", "from@x"); s != nil {
		t.Errorf("expected nil sender for empty host, got %+v", s)
	}
}

func TestNewSender_NonEmptyReturnsSender(t *testing.T) {
	if s := email.NewSender("smtp.example", 25, "u", "p", "f@x"); s == nil {
		t.Error("expected non-nil sender for non-empty host")
	}
}

func TestSender_Send_NilReturnsError(t *testing.T) {
	var s *email.Sender
	err := s.Send("to@x", "subj", "body")
	if err == nil {
		t.Fatal("expected error from nil Sender.Send")
	}
	if !strings.Contains(err.Error(), "SMTP") {
		t.Errorf("error should mention SMTP, got %q", err.Error())
	}
}

func TestSender_Send_BadHostFails(t *testing.T) {
	// Hostname guaranteed not to resolve / connect; surfaces an error from net/smtp.
	s := email.NewSender("127.0.0.1", 1, "u", "p", "from@x")
	if s == nil {
		t.Fatal("sender should not be nil")
	}
	if err := s.Send("to@x", "s", "b"); err == nil {
		t.Error("expected error sending to unreachable SMTP")
	}
}
