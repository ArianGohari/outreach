package outreach

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"
)

type localSMTP struct {
	listener   net.Listener
	cert       tls.Certificate
	roots      *x509.CertPool
	mu         sync.Mutex
	messages   []string
	recipients []string
	mode       string
	wg         sync.WaitGroup
	ackRelease chan struct{}
}

func startSMTP(t *testing.T, mode string) *localSMTP {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	leaf, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	s := &localSMTP{listener: listener, cert: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, roots: roots, mode: mode}
	if mode == "hold-ack" {
		s.ackRelease = make(chan struct{})
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			s.wg.Add(1)
			go func() { defer s.wg.Done(); s.serve(conn) }()
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		if s.ackRelease != nil {
			select {
			case <-s.ackRelease:
			default:
				close(s.ackRelease)
			}
		}
		s.wg.Wait()
	})
	return s
}
func (s *localSMTP) serve(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if s.mode == "implicit" {
		secure := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{s.cert}, MinVersion: tls.VersionTLS12})
		if e := secure.Handshake(); e != nil {
			return
		}
		conn = secure
	}
	p := textproto.NewConn(conn)
	p.PrintfLine("220 localhost test SMTP")
	for {
		line, e := p.ReadLine()
		if e != nil {
			return
		}
		switch {
		case strings.HasPrefix(line, "EHLO"):
			if s.mode == "no-tls" {
				p.PrintfLine("250 localhost")
			} else {
				p.PrintfLine("250-localhost")
				p.PrintfLine("250-STARTTLS")
				p.PrintfLine("250 AUTH PLAIN")
			}
		case line == "STARTTLS":
			p.PrintfLine("220 Ready")
			secure := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{s.cert}, MinVersion: tls.VersionTLS12})
			if e := secure.Handshake(); e != nil {
				return
			}
			conn = secure
			p = textproto.NewConn(secure)
		case strings.HasPrefix(line, "AUTH"):
			if s.mode == "auth-reject" {
				p.PrintfLine("535 Authentication failed")
			} else {
				p.PrintfLine("235 Authenticated")
			}
		case line == "NOOP":
			p.PrintfLine("250 OK")
		case strings.HasPrefix(line, "MAIL FROM:"):
			p.PrintfLine("250 OK")
		case strings.HasPrefix(line, "RCPT TO:"):
			if s.mode == "reject" {
				p.PrintfLine("550 No such recipient")
			} else {
				s.mu.Lock()
				s.recipients = append(s.recipients, strings.TrimPrefix(line, "RCPT TO:"))
				s.mu.Unlock()
				p.PrintfLine("250 OK")
			}
		case line == "DATA":
			p.PrintfLine("354 End with dot")
			data, e := io.ReadAll(p.DotReader())
			if e != nil {
				return
			}
			s.mu.Lock()
			s.messages = append(s.messages, string(data))
			s.mu.Unlock()
			if s.ackRelease != nil {
				<-s.ackRelease
			}
			if s.mode == "drop-ack" {
				return
			}
			p.PrintfLine("250 Message accepted")
		case line == "QUIT":
			if s.mode != "drop-quit" {
				p.PrintfLine("221 Bye")
			}
			return
		default:
			p.PrintfLine("500 Unknown command")
		}
	}
}
func (s *localSMTP) config() Config {
	c := validRequest().Config
	c.Host = "127.0.0.1"
	c.Port = s.listener.Addr().(*net.TCPAddr).Port
	return c
}
func (s *localSMTP) sender() SMTPSender {
	return SMTPSender{tlsConfig: &tls.Config{RootCAs: s.roots, MinVersion: tls.VersionTLS12}}
}

func TestSMTPRealTLSAndMIME(t *testing.T) {
	server := startSMTP(t, "")
	c := server.config()
	c.FromName = "Arian Müller"
	c.ReplyTo = "reply@example.org"
	sender := server.sender()
	if e := sender.Check(context.Background(), c); e != nil {
		t.Fatal(e)
	}
	m := samplePlan(t).Messages[0]
	m.Subject = "Grüße, Lena – München"
	m.Body = "Hallo Lena,\n\nÄÖÜ & <kein HTML>\n.Punkt\n\nViele Grüße\n"
	result := sender.Send(context.Background(), c, m)
	if result.Status != "sent" {
		t.Fatal(result)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.recipients) != 1 || server.recipients[0] != "<"+m.To+">" || len(server.messages) != 1 {
		t.Fatal("wrong envelope")
	}
	parsed, e := mail.ReadMessage(bufio.NewReader(strings.NewReader(server.messages[0])))
	if e != nil {
		t.Fatal(e)
	}
	if parsed.Header.Get("Content-Type") != "text/plain; charset=UTF-8" {
		t.Fatal("SMTP text format changed")
	}
	subject, e := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if e != nil || subject != m.Subject {
		t.Fatal("subject encoding changed", subject, e)
	}
	body, e := io.ReadAll(quotedprintable.NewReader(parsed.Body))
	if e != nil || strings.ReplaceAll(string(body), "\r\n", "\n") != m.Body {
		t.Fatalf("body changed: %q / %v", body, e)
	}
	if parsed.Header.Get("Bcc") != "" || parsed.Header.Get("Cc") != "" || parsed.Header.Get("Reply-To") != "<reply@example.org>" || parsed.Header.Get("Message-ID") != m.MessageID {
		t.Fatal("headers mismatch", parsed.Header)
	}
}
func TestSMTPFailureClassification(t *testing.T) {
	for _, tc := range []struct{ mode, status string }{{"drop-ack", "uncertain"}, {"drop-quit", "sent"}, {"reject", "failed"}, {"auth-reject", "failed"}, {"no-tls", "failed"}} {
		t.Run(tc.mode, func(t *testing.T) {
			s := startSMTP(t, tc.mode)
			result := s.sender().Send(context.Background(), s.config(), samplePlan(t).Messages[0])
			if result.Status != tc.status {
				t.Fatal(result)
			}
		})
	}
}
func TestSMTPRejectsUntrustedCertificate(t *testing.T) {
	s := startSMTP(t, "")
	if e := (SMTPSender{}).Check(context.Background(), s.config()); e == nil {
		t.Fatal("untrusted TLS certificate accepted")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) != 0 {
		t.Fatal("message sent over untrusted connection")
	}
}
func TestMIMERejectsHeaderInjection(t *testing.T) {
	c := validRequest().Config
	m := samplePlan(t).Messages[0]
	m.To = "test@example.com\r\nBcc: victim@example.com"
	if _, e := MIME(c, m); e == nil {
		t.Fatal("recipient injection accepted")
	}
	m = samplePlan(t).Messages[0]
	m.Subject = "Hi\r\nBcc: victim@example.com"
	if _, e := MIME(c, m); e == nil {
		t.Fatal("subject injection accepted")
	}
}

func TestSMTPImplicitTLS465(t *testing.T) {
	s := startSMTP(t, "implicit")
	c := s.config()
	c.Port = 465
	sender := s.sender()
	sender.dialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if !strings.HasSuffix(address, ":465") {
			t.Errorf("incorrect implicit TLS port: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, s.listener.Addr().String())
	}
	if e := sender.Check(context.Background(), c); e != nil {
		t.Fatal(e)
	}
	if result := sender.Send(context.Background(), c, samplePlan(t).Messages[0]); result.Status != "sent" {
		t.Fatal(result)
	}
}
func TestFullWorkflowAgainstLocalSMTP(t *testing.T) {
	server := startSMTP(t, "")
	sender := server.sender()
	store := testStore(t)
	engine := NewEngine(store, sender, newClock(), nil)
	session := NewSession(engine, sender, Report{})
	req := validRequest()
	req.Config = server.config()
	if _, e := session.Import(fixture(t), "synthetic.csv", false); e != nil {
		t.Fatal(e)
	}
	if e := session.Check(context.Background(), req.Config); e != nil {
		t.Fatal(e)
	}
	plan, e := session.Preview(req)
	if e != nil {
		t.Fatal(e)
	}
	token, e := session.Test(context.Background(), req, 0, "test@example.org")
	if e != nil {
		t.Fatal(e)
	}
	if e = session.Start(req, token); e != nil {
		t.Fatal(e)
	}
	waitEngine(t, engine)
	if e = session.Start(req, token); e != nil {
		t.Fatal(e)
	}
	waitEngine(t, engine)
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.messages) != 4 {
		t.Fatalf("expected 1 test and 3 unique outreach messages, got %d", len(server.messages))
	}
	for i, m := range plan.Messages {
		parsed, e := mail.ReadMessage(strings.NewReader(server.messages[i+1]))
		if e != nil {
			t.Fatal(e)
		}
		to, e := parsed.Header.AddressList("To")
		if e != nil || len(to) != 1 || to[0].Address != m.To {
			t.Fatal("recipient mismatch")
		}
		body, e := io.ReadAll(quotedprintable.NewReader(parsed.Body))
		if e != nil || string(body) != m.Body {
			t.Fatal("preview and delivered body differ")
		}
	}
}
