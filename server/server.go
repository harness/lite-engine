// Package server provides an HTTPS server with support for TLS
// and graceful shutdown.
package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"

	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

// A Server defines parameters for running an HTTPS/TLS server.
type Server struct {
	Addr           string // TCP address to listen on
	Handler        http.Handler
	CertFile       string // Server certificate PEM file
	KeyFile        string // Server key PEM file
	ClientCertFile string // Trusted client certificate PEM file for client authentication
}

// Start initializes a server to respond to HTTPS/TLS network requests.
func (s Server) Start(ctx context.Context) error {
	// Trusted client certificate.
	clientCert, err := os.ReadFile(s.ClientCertFile)
	if err != nil {
		return errors.Wrapf(err, fmt.Sprintf("failed to read client certificate file at path: %s", s.ClientCertFile))
	}
	clientCertPool := x509.NewCertPool()
	clientCertPool.AppendCertsFromPEM(clientCert)

	srv := &http.Server{
		Addr:    s.Addr,
		Handler: s.Handler,
		TLSConfig: &tls.Config{
			MinVersion:               tls.VersionTLS13,
			PreferServerCipherSuites: true,
			ClientCAs:                clientCertPool,
			ClientAuth:               tls.RequireAndVerifyClientCert,
		},
	}

	var g errgroup.Group
	g.Go(func() error {
		return srv.ListenAndServeTLS(s.CertFile, s.KeyFile)
	})
	g.Go(func() error {
		select {
		case <-ctx.Done():
			srv.Shutdown(ctx)
			return nil
		}
	})
	return g.Wait()
}
