package deliver

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/smtp"
	"slices"
	"strings"
)

// SMTP sends mail through a plain SMTP server. It implements MailSender
// and can be passed to NewEmail.
type SMTP struct {
	Host string
	Port string
	User string
	Pass string
}

func NewSMTP(host, port, user, pass string) *SMTP {
	return &SMTP{Host: host, Port: port, User: user, Pass: pass}
}

const mimeBoundary = "----=_invoice_app_boundary"

func (s *SMTP) Send(from string, to []string, subject, body string, attachments map[string][]byte) error {
	if len(to) == 0 {
		return fmt.Errorf("smtp: no recipients")
	}
	msg := buildMessage(from, to, subject, body, attachments)
	addr := s.Host + ":" + s.Port

	var auth smtp.Auth
	if s.User != "" {
		auth = smtp.PlainAuth("", s.User, s.Pass, s.Host)
	}
	return smtp.SendMail(addr, auth, from, to, msg)
}

func buildMessage(from string, to []string, subject, body string, attachments map[string][]byte) []byte {
	var b strings.Builder

	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")

	names := sortedKeys(attachments)

	b.WriteString("Content-Type: multipart/mixed; boundary=" + mimeBoundary + "\r\n")
	b.WriteString("\r\n")

	writeTextPart(&b, body)
	for _, name := range names {
		writeAttachment(&b, name, attachments[name])
	}
	b.WriteString("--" + mimeBoundary + "--\r\n")

	return []byte(b.String())
}

func writeTextPart(b *strings.Builder, body string) {
	fmt.Fprintf(b, "--%s\r\n", mimeBoundary)
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	b.WriteString("\r\n")
}

func writeAttachment(b *strings.Builder, filename string, data []byte) {
	fmt.Fprintf(b, "--%s\r\n", mimeBoundary)
	b.WriteString("Content-Type: application/pdf; name=\"" + filename + "\"\r\n")
	b.WriteString("Content-Disposition: attachment; filename=\"" + filename + "\"\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString("\r\n")
	b.WriteString(base64Wrap(data))
	b.WriteString("\r\n")
}

// base64Wrap encodes data as standard base64 broken into 76-char lines,
// as required by MIME.
func base64Wrap(data []byte) string {
	enc := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	for len(enc) > 76 {
		b.WriteString(enc[:76])
		b.WriteString("\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc)
	return b.String()
}

func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
