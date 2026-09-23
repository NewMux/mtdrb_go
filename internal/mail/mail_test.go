package mail

import (
	"context"
	"strings"
	"testing"
)

func TestRenderEncodesAnArabicSubject(t *testing.T) {
	out := string(render("no-reply@coachpulse.io", Message{
		To: "sam@example.com", Subject: "إعادة تعيين كلمة المرور", Text: "line one\nline two",
	}))
	if !strings.Contains(out, "Subject: =?utf-8?q?") {
		t.Fatalf("subject not encoded:\n%s", out)
	}
	if !strings.Contains(out, "line one\r\nline two") {
		t.Fatalf("body line endings not normalised:\n%s", out)
	}
}

func TestSendRefusesHeaderInjection(t *testing.T) {
	err := SMTPSender{Host: "127.0.0.1", Port: 1}.Send(context.Background(), Message{
		To: "a@example.com\r\nBcc: everyone@example.com", Subject: "x", Text: "x",
	})
	if err == nil || !strings.Contains(err.Error(), "line break") {
		t.Fatalf("expected a refusal, got %v", err)
	}
}
