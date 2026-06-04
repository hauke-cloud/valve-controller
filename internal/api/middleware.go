package api

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
)

// TLSConfig builds a *tls.Config that requires and verifies client certificates.
// certFile/keyFile are the server certificate and private key.
// clientCAFile is the PEM-encoded CA certificate used to verify client certs.
func TLSConfig(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load server cert/key: %w", err)
	}

	caPEM, err := os.ReadFile(clientCAFile)
	if err != nil {
		return nil, fmt.Errorf("read client CA: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("failed to parse client CA cert from %s", clientCAFile)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// requireClientCert is an HTTP middleware that rejects requests that did not
// present a valid client certificate. It is redundant when TLS is terminated
// at the Go server with RequireAndVerifyClientCert, but serves as a defence-
// in-depth layer when a reverse proxy terminates TLS.
func requireClientCert(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
			http.Error(w, `{"error":"client certificate required","code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
