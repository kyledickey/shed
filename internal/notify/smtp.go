package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SMTP returns the provider for email. Its target is
// smtp://[user:pass@]host[:port]?from=<addr>&to=<addr>[,<addr>...], which
// requires STARTTLS and defaults to port 587, or smtps://..., which uses
// implicit TLS and defaults to port 465.
func SMTP() Provider {
	return Provider{
		Name: "smtp",
		Open: func(target string) (Sender, error) { return openSMTP(target) },
	}
}

type smtpSender struct {
	host, addr  string
	implicitTLS bool
	user, pass  string
	from        *mail.Address
	to          []*mail.Address
	roots       *x509.CertPool // nil uses the system roots; tests set it
}

// openSMTP parses a target URL. Errors never echo the URL, since it may hold
// a password.
func openSMTP(target string) (smtpSender, error) {
	u, err := url.Parse(target)
	if err != nil {
		return smtpSender{}, errors.New("smtp: target is not a valid URL")
	}
	s := smtpSender{host: u.Hostname()}
	port := u.Port()
	switch u.Scheme {
	case "smtp":
		if port == "" {
			port = "587"
		}
	case "smtps":
		s.implicitTLS = true
		if port == "" {
			port = "465"
		}
	default:
		return smtpSender{}, errors.New("smtp: target must use smtp:// or smtps://")
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return smtpSender{}, errors.New("smtp: target port is not valid")
	}
	if s.host == "" {
		return smtpSender{}, errors.New("smtp: target has no host")
	}
	if (u.Path != "" && u.Path != "/") || u.Fragment != "" {
		return smtpSender{}, errors.New("smtp: target must not have a path or a fragment")
	}
	s.addr = net.JoinHostPort(s.host, port)
	if u.User != nil {
		s.user = u.User.Username()
		s.pass, _ = u.User.Password()
	}

	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return smtpSender{}, errors.New("smtp: target query is not valid")
	}
	for k, v := range q {
		if k != "from" && k != "to" {
			return smtpSender{}, fmt.Errorf("smtp: unknown parameter %q", k)
		}
		if len(v) != 1 {
			return smtpSender{}, fmt.Errorf("smtp: parameter %q must appear once", k)
		}
	}
	if s.from, err = mail.ParseAddress(q.Get("from")); err != nil {
		return smtpSender{}, errors.New("smtp: from must be one email address")
	}
	if s.to, err = mail.ParseAddressList(q.Get("to")); err != nil {
		return smtpSender{}, errors.New("smtp: to must be a comma-separated list of email addresses")
	}
	return s, nil
}

// Send delivers m as a plain text email. Plain SMTP without TLS is allowed
// only to a loopback host, and never with credentials.
func (s smtpSender) Send(ctx context.Context, m Message) error {
	msg, err := s.message(m)
	if err != nil {
		return fmt.Errorf("smtp: build message: %w", err)
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("smtp: dial: %w", err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()

	cfg := &tls.Config{ServerName: s.host, RootCAs: s.roots}
	if s.implicitTLS {
		conn = tls.Client(conn, cfg)
	}
	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		conn.Close()
		return s.fail(ctx, "connect", err)
	}
	defer c.Close()

	if !s.implicitTLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(cfg); err != nil {
				return s.fail(ctx, "starttls", err)
			}
		} else if !isLoopback(s.host) || s.user != "" {
			return permanent(errors.New("smtp: server does not offer STARTTLS"))
		}
	}
	if s.user != "" {
		if err := c.Auth(smtp.PlainAuth("", s.user, s.pass, s.host)); err != nil {
			return s.fail(ctx, "auth", err)
		}
	}
	if err := c.Mail(s.from.Address); err != nil {
		return s.fail(ctx, "mail from", err)
	}
	for _, to := range s.to {
		if err := c.Rcpt(to.Address); err != nil {
			return s.fail(ctx, "rcpt to", err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return s.fail(ctx, "data", err)
	}
	if _, err := w.Write(msg); err != nil {
		return s.fail(ctx, "data", err)
	}
	if err := w.Close(); err != nil {
		return s.fail(ctx, "data", err)
	}
	_ = c.Quit()
	return nil
}

// fail wraps err from step, marking a 5xx reply as permanent. A canceled
// ctx shows up as an I/O timeout, so its error is reported instead.
func (s smtpSender) fail(ctx context.Context, step string, err error) error {
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	te, ok := errors.AsType[*textproto.Error](err)
	if s.pass != "" && strings.Contains(err.Error(), s.pass) {
		err = errors.New(strings.ReplaceAll(err.Error(), s.pass, "<password>"))
	}
	err = fmt.Errorf("smtp: %s: %w", step, err)
	if ok && te.Code >= 500 {
		return permanent(err)
	}
	return err
}

func (s smtpSender) message(m Message) ([]byte, error) {
	subject := strings.NewReplacer("\r", "", "\n", "").Replace(m.Title)
	if subject == "" {
		subject = "shed"
	}
	to := make([]string, len(s.to))
	for i, a := range s.to {
		to[i] = a.String()
	}
	_, domain, _ := strings.Cut(s.from.Address, "@")

	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", s.from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@%s>\r\n", rand.Text(), domain)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")

	parts := []string{strings.TrimSpace(m.Body)}
	if m.Project != "" {
		parts = append(parts, "Project: "+m.Project)
	}
	if m.URL != "" {
		parts = append(parts, m.URL)
	}
	text := strings.ReplaceAll(strings.Join(parts, "\n\n"), "\r\n", "\n")
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(strings.ReplaceAll(text, "\n", "\r\n"))); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	b.WriteString("\r\n")
	return b.Bytes(), nil
}
