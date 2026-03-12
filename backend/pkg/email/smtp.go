package email

import (
	"fmt"
	"net/smtp"
)

// Sender sends emails via SMTP.
type Sender struct {
	host     string
	port     int
	user     string
	password string
	from     string
}

// NewSender creates a new SMTP email sender. Returns nil if host is empty (SMTP disabled).
func NewSender(host string, port int, user, password, from string) *Sender {
	if host == "" {
		return nil
	}
	return &Sender{host: host, port: port, user: user, password: password, from: from}
}

// Send sends a plain-text email. Returns an error if SMTP is not configured.
func (s *Sender) Send(to, subject, body string) error {
	if s == nil {
		return fmt.Errorf("SMTP not configured")
	}
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	auth := smtp.PlainAuth("", s.user, s.password, s.host)
	msg := []byte(
		"From: " + s.from + "\r\n" +
			"To: " + to + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"Content-Type: text/plain; charset=UTF-8\r\n" +
			"\r\n" +
			body + "\r\n",
	)
	return smtp.SendMail(addr, auth, s.from, []string{to}, msg)
}
