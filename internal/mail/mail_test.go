package mail

import (
	"bufio"
	"context"
	"fmt"
	"net"
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

// plainSMTP is a mail server that does not offer STARTTLS, as one looks
// after someone on the path strips the offer from its greeting.
func plainSMTP(t *testing.T) (host string, port int, got chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got = make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		r := bufio.NewReader(conn)
		fmt.Fprint(conn, "220 test ESMTP\r\n")
		var seen []string
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				got <- strings.Join(seen, "|")
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			seen = append(seen, cmd)
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				fmt.Fprint(conn, "250-test\r\n250 8BITMIME\r\n")
			case cmd == "DATA":
				fmt.Fprint(conn, "354 go ahead\r\n")
				for {
					body, err := r.ReadString('\n')
					if err != nil || body == ".\r\n" {
						break
					}
				}
				fmt.Fprint(conn, "250 queued\r\n")
			case strings.HasPrefix(cmd, "QUIT"):
				fmt.Fprint(conn, "221 bye\r\n")
				got <- strings.Join(seen, "|")
				return
			default:
				fmt.Fprint(conn, "250 ok\r\n")
			}
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, got
}

func TestSendRefusesAServerWithoutSTARTTLS(t *testing.T) {
	host, port, got := plainSMTP(t)
	s := SMTPSender{Host: host, Port: port, From: "no-reply@coachpulse.test"}
	err := s.Send(context.Background(), Message{To: "sam@example.com", Subject: "Reset", Text: "https://app/reset?token=secret"})
	if err == nil || !strings.Contains(err.Error(), "does not offer STARTTLS") {
		t.Fatalf("sent without TLS: %v", err)
	}
	if cmds := <-got; strings.Contains(cmds, "MAIL FROM") || strings.Contains(cmds, "DATA") {
		t.Errorf("the message reached a plaintext server: %s", cmds)
	}
}

func TestSendInTheClearOnlyWhenAskedTo(t *testing.T) {
	host, port, got := plainSMTP(t)
	s := SMTPSender{Host: host, Port: port, From: "no-reply@coachpulse.test", TLS: TLSNone}
	if err := s.Send(context.Background(), Message{To: "sam@example.com", Subject: "Reset", Text: "hi"}); err != nil {
		t.Fatalf("a local mail catcher should accept the message: %v", err)
	}
	if cmds := <-got; !strings.Contains(cmds, "DATA") {
		t.Errorf("message not sent: %s", cmds)
	}
}
