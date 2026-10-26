//go:build integration

package outreach

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
)

// IntegrationSender is excluded from release builds and cannot dial outside its loopback fixture.
func IntegrationSender(roots *x509.CertPool, endpoint string) SMTPSender {
	return SMTPSender{tlsConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, dialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || address != endpoint {
			return nil, fmt.Errorf("integration SMTP must use its loopback fixture")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
}
