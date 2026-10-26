//go:build integration

// Package testsupport provides a local TLS SMTP sink. It is excluded from release builds.
package testsupport

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"time"
)

type SMTP struct {
	Listener   net.Listener
	Roots      *x509.CertPool
	cert       tls.Certificate
	mu         sync.Mutex
	messages   []string
	recipients []string
	wg         sync.WaitGroup
}

func NewSMTP() (*SMTP, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	c := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "localhost"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, c, c, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	s := &SMTP{Listener: listener, Roots: roots, cert: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}}
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
	return s, nil
}
func (s *SMTP) Close() { s.Listener.Close(); s.wg.Wait() }
func (s *SMTP) Snapshot() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.messages...), append([]string(nil), s.recipients...)
}
func (s *SMTP) serve(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	p := textproto.NewConn(conn)
	p.PrintfLine("220 local integration sink")
	secure, authed := false, false
	recipient := ""
	for {
		line, err := p.ReadLine()
		if err != nil {
			return
		}
		switch {
		case strings.HasPrefix(line, "EHLO"):
			if secure {
				p.PrintfLine("250-localhost")
				p.PrintfLine("250 AUTH PLAIN")
			} else {
				p.PrintfLine("250-localhost")
				p.PrintfLine("250 STARTTLS")
			}
		case line == "STARTTLS" && !secure:
			p.PrintfLine("220 Ready")
			tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{s.cert}, MinVersion: tls.VersionTLS12})
			if tlsConn.Handshake() != nil {
				return
			}
			conn = tlsConn
			p = textproto.NewConn(conn)
			secure = true
		case strings.HasPrefix(line, "AUTH PLAIN ") && secure:
			authed = true
			p.PrintfLine("235 Authenticated")
		case line == "NOOP":
			p.PrintfLine("250 OK")
		case strings.HasPrefix(line, "MAIL FROM:") && authed:
			p.PrintfLine("250 OK")
		case strings.HasPrefix(line, "RCPT TO:") && authed:
			recipient = strings.TrimPrefix(line, "RCPT TO:")
			p.PrintfLine("250 OK")
		case line == "DATA" && authed && recipient != "":
			p.PrintfLine("354 End with dot")
			data, e := io.ReadAll(p.DotReader())
			if e != nil {
				return
			}
			s.mu.Lock()
			s.messages = append(s.messages, string(data))
			s.recipients = append(s.recipients, recipient)
			s.mu.Unlock()
			p.PrintfLine("250 Accepted")
		case line == "QUIT":
			p.PrintfLine("221 Bye")
			return
		default:
			p.PrintfLine("500 Unsupported test command")
		}
	}
}
