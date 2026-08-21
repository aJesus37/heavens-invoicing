package deliver

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestBuildMessageTextOnly(t *testing.T) {
	msg := string(buildMessage("from@me.com", []string{"to@acme.com"}, "Fatura #000001", "corpo da mensagem", nil))

	for _, want := range []string{
		"From: from@me.com\r\n",
		"To: to@acme.com\r\n",
		"MIME-Version: 1.0\r\n",
		`Content-Type: multipart/mixed; boundary=` + mimeBoundary,
		`Content-Type: text/plain; charset="utf-8"`,
		"\r\n\r\ncorpo da mensagem\r\n",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q does not contain %q", msg, want)
		}
	}
	if strings.Contains(msg, "application/pdf") {
		t.Fatalf("no attachment expected, got %q", msg)
	}
	if n := strings.Count(msg, "--"+mimeBoundary+"\r\n"); n != 1 {
		t.Fatalf("want exactly 1 part opener, got %d in %q", n, msg)
	}
	if !strings.HasSuffix(msg, "--"+mimeBoundary+"--\r\n") {
		t.Fatalf("missing terminating boundary in %q", msg)
	}
}

func TestBuildMessageWithAttachment(t *testing.T) {
	data := make([]byte, 500)
	for i := range data {
		data[i] = byte(i * 7)
	}
	msg := string(buildMessage("from@me.com", []string{"to@acme.com"}, "Fatura #000001", "corpo", map[string][]byte{"fatura-000001.pdf": data}))

	for _, want := range []string{
		`Content-Type: application/pdf; name="fatura-000001.pdf"`,
		`Content-Disposition: attachment; filename="fatura-000001.pdf"`,
		"Content-Transfer-Encoding: base64",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q does not contain %q", msg, want)
		}
	}

	b64 := extractBase64(t, msg)
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("decode attachment: %v", err)
	}
	if !bytes.Equal(decoded, data) {
		t.Fatal("decoded attachment differs from original bytes")
	}

	for _, line := range strings.Split(strings.TrimSuffix(b64, "\r\n"), "\r\n") {
		if len(line) > 76 {
			t.Fatalf("base64 line exceeds 76 chars (%d): %q", len(line), line)
		}
	}

	if n := strings.Count(msg, "--"+mimeBoundary+"\r\n"); n != 2 {
		t.Fatalf("want 2 part openers (text+attachment), got %d", n)
	}
	if !strings.HasSuffix(msg, "--"+mimeBoundary+"--\r\n") {
		t.Fatalf("missing terminating boundary in %q", msg)
	}
}

func TestBase64Wrap(t *testing.T) {
	for _, n := range []int{0, 1, 57, 76, 77, 200, 1000} {
		data := make([]byte, n)
		for i := range data {
			data[i] = byte(i)
		}
		got, err := base64.StdEncoding.DecodeString(base64Wrap(data))
		if err != nil {
			t.Fatalf("n=%d: decode: %v", n, err)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("n=%d: round-trip mismatch", n)
		}
	}
}

func TestSMTPSendNoRecipients(t *testing.T) {
	s := NewSMTP("localhost", "2525", "", "")
	if err := s.Send("from@me.com", nil, "subject", "body", nil); err == nil {
		t.Fatal("want error for empty recipient list, got nil")
	}
}

// extractBase64 pulls the base64 payload of the last MIME part.
func extractBase64(t *testing.T, msg string) string {
	t.Helper()
	marker := "Content-Transfer-Encoding: base64\r\n\r\n"
	i := strings.LastIndex(msg, marker)
	if i < 0 {
		t.Fatalf("no base64 part found in %q", msg)
	}
	payload := msg[i+len(marker):]
	if end := strings.Index(payload, "\r\n--"+mimeBoundary); end >= 0 {
		payload = payload[:end]
	}
	return payload
}
