// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package certs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alecthomas/kingpin/v2"
	"github.com/harness/godotenv/v3"
	"github.com/harness/lite-engine/config"
	"github.com/sirupsen/logrus"
)

const certPermissions = os.FileMode(0600)

type certCommand struct {
	certPath string
	envfile  string
}

func generateCert(serverName, relPath string) error {
	ca, err := GenerateCA()
	if err != nil {
		return fmt.Errorf("failed to generate ca certificate: %w", err)
	}

	tlsCert, err := GenerateCert(serverName, ca)
	if err != nil {
		return fmt.Errorf("failed to generate certificate: %w", err)
	}

	err = os.MkdirAll(relPath, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create directory at path: %s: %w", relPath, err)
	}

	caCertFilePath := filepath.Join(relPath, "ca-cert.pem")
	caKeyFilePath := filepath.Join(relPath, "ca-key.pem")
	if err := os.WriteFile(caCertFilePath, ca.Cert, certPermissions); err != nil {
		return fmt.Errorf("failed to write CA cert file: %w", err)
	}
	if err := os.WriteFile(caKeyFilePath, ca.Key, certPermissions); err != nil {
		return fmt.Errorf("failed to write CA key file: %w", err)
	}

	certFilePath := filepath.Join(relPath, "server-cert.pem")
	keyFilePath := filepath.Join(relPath, "server-key.pem")
	if err := os.WriteFile(certFilePath, tlsCert.Cert, certPermissions); err != nil {
		return fmt.Errorf("failed to write server cert file: %w", err)
	}
	if err := os.WriteFile(keyFilePath, tlsCert.Key, certPermissions); err != nil {
		return fmt.Errorf("failed to write server key file: %w", err)
	}
	return nil
}

func (c *certCommand) run(*kingpin.ParseContext) error {
	loadEnvErr := godotenv.Load(c.envfile)
	if loadEnvErr != nil {
		logrus.
			WithError(loadEnvErr).
			Errorln("cannot load env file")
	}
	// load the system configuration from the environment.
	loadedConfig, err := config.Load()
	if err != nil {
		logrus.WithError(err).
			Errorln("cannot load the service configuration")
		return err
	}

	return generateCert(loadedConfig.ServerName, c.certPath)
}

// Register the server commands.
func Register(app *kingpin.Application) {
	c := new(certCommand)

	cmd := app.Command("certs", "generates the TLS certificates for local testing").
		Action(c.run)

	cmd.Flag("certPath", "Directory to generate the TLS certificates").
		Default("/tmp/certs").
		StringVar(&c.certPath)
	cmd.Flag("env-file", "environment file").
		Default(".env").
		StringVar(&c.envfile)
}
