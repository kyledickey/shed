package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"mime/quotedprintable"
	"net"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"strings"
	"sync"
	"testing"
)

const smtpPass = "p4ss_s3cretS3cret"

// smtpFake is a minimal SMTP server on loopback. It offers STARTTLS (or
// implicit TLS) when tls is set, AUTH PLAIN once the connection is secure,
// and replies to the verbs in codes with that code instead.
type smtpFake struct {
	addr     string
	roots    *x509.CertPool
	tls      *tls.Config
	implicit bool
	codes    map[string]int

	mu         sync.Mutex
	auth       string
	secureMail bool
	from       string
	to         []string
	data       string
}

func newSMTPFake(t *testing.T, useTLS, implicit bool, codes map[string]int) *smtpFake {
	t.Helper()
	f := &smtpFake{implicit: implicit, codes: codes}
	if useTLS {
		ts := httptest.NewTLSServer(nil)
		f.tls = &tls.Config{Certificates: ts.TLS.Certificates}
		f.roots = x509.NewCertPool()
		f.roots.AddCert(ts.Certificate())
		ts.Close()
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f.addr = ln.Addr().String()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *smtpFake) serve(c net.Conn) {
	defer func() { c.Close() }()
	secure := f.implicit
	if secure {
		c = tls.Server(c, f.tls)
	}
	tp := textproto.NewConn(c)
	_ = tp.PrintfLine("220 fake ESMTP")
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		verb = strings.ToUpper(verb)
		if code, ok := f.codes[verb]; ok {
			_ = tp.PrintfLine("%d rejected %s", code, arg)
			continue
		}
		f.mu.Lock()
		switch verb {
		case "EHLO", "HELO":
			ext := []string{"fake"}
			if f.tls != nil && !secure {
				ext = append(ext, "STARTTLS")
			}
			if secure {
				ext = append(ext, "AUTH PLAIN")
			}
			for i, e := range ext {
				sep := "-"
				if i == len(ext)-1 {
					sep = " "
				}
				_ = tp.PrintfLine("250%s%s", sep, e)
			}
		case "STARTTLS":
			_ = tp.PrintfLine("220 go ahead")
			c = tls.Server(c, f.tls)
			tp = textproto.NewConn(c)
			secure = true
		case "AUTH":
			_, b64, _ := strings.Cut(arg, " ")
			raw, _ := base64.StdEncoding.DecodeString(b64)
			f.auth = string(raw)
			_ = tp.PrintfLine("235 ok")
		case "MAIL":
			f.from, f.secureMail = arg, secure
			_ = tp.PrintfLine("250 ok")
		case "RCPT":
			f.to = append(f.to, arg)
			_ = tp.PrintfLine("250 ok")
		case "DATA":
			_ = tp.PrintfLine("354 go ahead")
			raw, _ := tp.ReadDotBytes()
			f.data = string(raw)
			_ = tp.PrintfLine("250 queued")
		case "QUIT":
			_ = tp.PrintfLine("221 bye")
			f.mu.Unlock()
			return
		default:
			_ = tp.PrintfLine("502 unknown")
		}
		f.mu.Unlock()
	}
}

func (f *smtpFake) sender(t *testing.T, scheme, userinfo string) smtpSender {
	t.Helper()
	s, err := openSMTP(scheme + "://" + userinfo + f.addr +
		"?from=Shed+%3Cshed@example.com%3E&to=a@example.com,b@example.com")
	if err != nil {
		t.Fatal(err)
	}
	s.roots = f.roots
	return s
}

func TestOpenSMTP(t *testing.T) {
	const q = "?from=shed@example.com&to=a@example.com"
	tests := []struct {
		url      string
		ok       bool
		addr     string
		implicit bool
	}{
		{"smtp://mail.example.com" + q, true, "mail.example.com:587", false},
		{"smtps://mail.example.com" + q, true, "mail.example.com:465", true},
		{"smtp://u:" + smtpPass + "@mail.example.com:2525/" + q, true, "mail.example.com:2525", false},
		{"smtp://mail.example.com?from=Shed+%3Cshed@example.com%3E&to=a@example.com,%22B%22+%3Cb@example.com%3E", true, "mail.example.com:587", false},
		{"http://mail.example.com" + q, false, "", false},
		{"smtp://" + q, false, "", false},
		{"smtp://u:" + smtpPass + "@mail.example.com/path" + q, false, "", false},
		{"smtp://u:" + smtpPass + "@mail.example.com" + q + "#frag", false, "", false},
		{"smtp://mail.example.com?to=a@example.com", false, "", false},
		{"smtp://mail.example.com?from=shed@example.com", false, "", false},
		{"smtp://mail.example.com?from=a@example.com,b@example.com&to=a@example.com", false, "", false},
		{"smtp://mail.example.com?from=nope&to=a@example.com", false, "", false},
		{"smtp://mail.example.com?from=shed@example.com&to=nope", false, "", false},
		{"smtp://mail.example.com" + q + "&from=x@example.com", false, "", false},
		{"smtp://mail.example.com" + q + "&cc=x@example.com", false, "", false},
		{"smtp://u:" + smtpPass + "@mail.example.com:99999" + q, false, "", false},
	}
	for _, tt := range tests {
		s, err := openSMTP(tt.url)
		if (err == nil) != tt.ok {
			t.Errorf("openSMTP(%q) error = %v, want ok %v", tt.url, err, tt.ok)
		}
		if err != nil && strings.Contains(err.Error(), smtpPass) {
			t.Errorf("openSMTP(%q) error %q contains the password", tt.url, err)
		}
		if err == nil && (s.addr != tt.addr || s.implicitTLS != tt.implicit) {
			t.Errorf("openSMTP(%q) = addr %q implicit %v, want %q %v", tt.url, s.addr, s.implicitTLS, tt.addr, tt.implicit)
		}
	}
}

func TestSMTPSendMessage(t *testing.T) {
	f := newSMTPFake(t, false, false, nil)
	err := f.sender(t, "smtp", "").Send(context.Background(), Message{
		Title:   "Deploy failed: wéb",
		Body:    "  Deploy failed.\nSee logs.  ",
		URL:     "https://shed.example/deployments/1",
		Project: "acme",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.from != "FROM:<shed@example.com>" {
		t.Errorf("MAIL %q", f.from)
	}
	if strings.Join(f.to, " ") != "TO:<a@example.com> TO:<b@example.com>" {
		t.Errorf("RCPT %q", f.to)
	}
	if f.auth != "" {
		t.Errorf("AUTH sent without credentials: %q", f.auth)
	}
	msg, err := mail.ReadMessage(strings.NewReader(f.data))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"From":                      `"Shed" <shed@example.com>`,
		"To":                        "<a@example.com>, <b@example.com>",
		"Subject":                   "=?utf-8?q?Deploy_failed:_w=C3=A9b?=",
		"Mime-Version":              "1.0",
		"Content-Type":              "text/plain; charset=utf-8",
		"Content-Transfer-Encoding": "quoted-printable",
	}
	for k, v := range want {
		if got := msg.Header.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if _, err := msg.Header.Date(); err != nil {
		t.Errorf("Date: %v", err)
	}
	if id := msg.Header.Get("Message-Id"); !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, "@example.com>") {
		t.Errorf("Message-ID = %q", id)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if err != nil {
		t.Fatal(err)
	}
	// ReadDotBytes turns the CRLF line endings into LF.
	wantBody := "Deploy failed.\nSee logs.\n\nProject: acme\n\nhttps://shed.example/deployments/1\n"
	if string(body) != wantBody {
		t.Errorf("body = %q, want %q", body, wantBody)
	}
}

func TestSMTPSendEmptyTitle(t *testing.T) {
	f := newSMTPFake(t, false, false, nil)
	if err := f.sender(t, "smtp", "").Send(context.Background(), Message{Title: "a\r\nBcc: x", Body: "hi"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	msg, err := mail.ReadMessage(strings.NewReader(f.data))
	if err != nil {
		t.Fatal(err)
	}
	if got := msg.Header.Get("Subject"); got != "aBcc: x" || msg.Header.Get("Bcc") != "" {
		t.Errorf("Subject = %q, Bcc = %q", got, msg.Header.Get("Bcc"))
	}
}

func TestSMTPSendTLSAuth(t *testing.T) {
	for _, scheme := range []string{"smtp", "smtps"} {
		t.Run(scheme, func(t *testing.T) {
			f := newSMTPFake(t, true, scheme == "smtps", nil)
			err := f.sender(t, scheme, "user:"+smtpPass+"@").Send(context.Background(), Message{Body: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if !f.secureMail {
				t.Error("MAIL was sent before TLS")
			}
			if f.auth != "\x00user\x00"+smtpPass {
				t.Errorf("AUTH PLAIN = %q", f.auth)
			}
			if !bytes.Contains([]byte(f.data), []byte("hi")) {
				t.Errorf("DATA = %q", f.data)
			}
		})
	}
}

func TestSMTPSendRefusesPlainAuth(t *testing.T) {
	f := newSMTPFake(t, false, false, nil)
	err := f.sender(t, "smtp", "user:"+smtpPass+"@").Send(context.Background(), Message{Body: "hi"})
	if !errors.Is(err, ErrPermanent) {
		t.Fatalf("err = %v, want permanent", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.auth != "" || f.from != "" {
		t.Errorf("sent AUTH %q, MAIL %q without TLS", f.auth, f.from)
	}
}

func TestSMTPSendStatus(t *testing.T) {
	tests := []struct {
		name  string
		codes map[string]int
		want  string
		perm  bool
	}{
		{name: "auth rejected", codes: map[string]int{"AUTH": 535}, want: "auth: 535", perm: true},
		{name: "recipient rejected", codes: map[string]int{"RCPT": 550}, want: "rcpt to: 550", perm: true},
		{name: "mailbox busy", codes: map[string]int{"RCPT": 451}, want: "rcpt to: 451"},
		{name: "sender deferred", codes: map[string]int{"MAIL": 421}, want: "mail from: 421"},
		{name: "data rejected", codes: map[string]int{"DATA": 554}, want: "data: 554", perm: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSMTPFake(t, true, false, tt.codes)
			err := f.sender(t, "smtp", "user:"+smtpPass+"@").Send(context.Background(), Message{Body: "hi"})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
			if strings.Contains(err.Error(), smtpPass) || strings.Contains(err.Error(), f.addr) {
				t.Errorf("error %q contains the password or target", err)
			}
			if got := errors.Is(err, ErrPermanent); got != tt.perm {
				t.Errorf("permanent = %v, want %v (err %v)", got, tt.perm, err)
			}
		})
	}
}

func TestSMTPSendClosedServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	s, err := SMTP().Open("smtp://user:" + smtpPass + "@" + addr + "?from=a@example.com&to=b@example.com")
	if err != nil {
		t.Fatal(err)
	}
	err = s.Send(context.Background(), Message{Body: "hi"})
	if err == nil || errors.Is(err, ErrPermanent) {
		t.Fatalf("err = %v, want a retryable error", err)
	}
	if strings.Contains(err.Error(), smtpPass) {
		t.Errorf("error %q contains the password", err)
	}
}

func TestSMTPSendCanceled(t *testing.T) {
	f := newSMTPFake(t, false, false, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := f.sender(t, "smtp", "").Send(ctx, Message{Body: "hi"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
