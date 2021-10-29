package certs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/drone/lite-engine/config"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

const certPermissions = os.FileMode(0600)

type certCommand struct {
	certPath string
	envfile  string
}

func generateCert(serverName, relPath string) error {
	ca, err := GenerateCA()
	if err != nil {
		return errors.Wrap(err, "failed to generate ca certificate")
	}

	tlsCert, err := GenerateCert(serverName, ca)
	if err != nil {
		return errors.Wrap(err, "failed to generate certificate")
	}

	err = os.MkdirAll(relPath, os.ModePerm)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to create directory at path: %s", relPath))
	}

	caCertFilePath := filepath.Join(relPath, "ca-cert.pem")
	caKeyFilePath := filepath.Join(relPath, "ca-key.pem")
	if err := os.WriteFile(caCertFilePath, ca.Cert, certPermissions); err != nil {
		return errors.Wrap(err, "failed to write CA cert file")
	}
	if err := os.WriteFile(caKeyFilePath, ca.Key, certPermissions); err != nil {
		return errors.Wrap(err, "failed to write CA key file")
	}

	certFilePath := filepath.Join(relPath, "server-cert.pem")
	keyFilePath := filepath.Join(relPath, "server-key.pem")
	if err := os.WriteFile(certFilePath, tlsCert.Cert, certPermissions); err != nil {
		return errors.Wrap(err, "failed to write server cert file")
	}
	if err := os.WriteFile(keyFilePath, tlsCert.Key, certPermissions); err != nil {
		return errors.Wrap(err, "failed to write server key file")
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
