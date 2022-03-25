// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Package server provides an HTTPS server with support for TLS
// and graceful shutdown.
package server

import (
	"context"
	"crypto/tls"
	"net/http"

	"github.com/docker/go-connections/tlsconfig"
	"golang.org/x/sync/errgroup"
)

// A Server defines parameters for running an HTTPS/TLS server.
type Server struct {
	Addr           string // TCP address to listen on
	Handler        http.Handler
	CAFile         string // CA certificate file
	CertFile       string // Server certificate PEM file
	KeyFile        string // Server key PEM file
	ClientCertFile string // Trusted client certificate PEM file for client authentication
}

// Start initializes a server to respond to HTTPS/TLS network requests.
func (s *Server) Start(ctx context.Context) error {
	tlsOptions := tlsconfig.Options{
		CAFile:             s.CAFile,
		CertFile:           s.CertFile,
		KeyFile:            s.KeyFile,
		ExclusiveRootPools: true,
	}

	tlsOptions.ClientAuth = tls.RequireAndVerifyClientCert
	tlsConfig, err := tlsconfig.Server(tlsOptions)
	if err != nil {
		return err
	}
	tlsConfig.MinVersion = tls.VersionTLS13

	srv := &http.Server{
		Addr:      s.Addr,
		Handler:   s.Handler,
		TLSConfig: tlsConfig,
	}

	var g errgroup.Group
	g.Go(func() error {
		return srv.ListenAndServeTLS(s.CertFile, s.KeyFile)
	})
	g.Go(func() error {
		<-ctx.Done()
		srv.Shutdown(ctx) // nolint: errcheck
		return nil
	})
	return g.Wait()
}
