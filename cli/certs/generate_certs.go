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

	relCaPath := filepath.Join(relPath, "ca")
	caCertFilePath := filepath.Join(relCaPath, "cert.pem")
	caKeyFilePath := filepath.Join(relCaPath, "key.pem")
	err = os.MkdirAll(relCaPath, os.ModePerm)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to create directory at path: %s", relCaPath))
	}

	if err := os.WriteFile(caCertFilePath, ca.Cert, 0600); err != nil {
		return errors.Wrap(err, "failed to write CA cert file")
	}
	if err := os.WriteFile(caKeyFilePath, ca.Key, 0600); err != nil {
		return errors.Wrap(err, "failed to write CA key file")
	}

	relTlsPath := filepath.Join(relPath, "tls")
	certFilePath := filepath.Join(relTlsPath, "cert.pem")
	keyFilePath := filepath.Join(relTlsPath, "key.pem")
	err = os.MkdirAll(relTlsPath, os.ModePerm)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to create directory at path: %s", relTlsPath))
	}

	if err := os.WriteFile(certFilePath, tlsCert.Cert, 0600); err != nil {
		return errors.Wrap(err, "failed to write cert file")
	}
	if err := os.WriteFile(keyFilePath, tlsCert.Key, 0600); err != nil {
		return errors.Wrap(err, "failed to write key file")
	}
	return nil
}

func (c *certCommand) run(*kingpin.ParseContext) error {
	godotenv.Load(c.envfile)

	// load the system configuration from the environment.
	config, err := config.Load()
	if err != nil {
		logrus.WithError(err).
			Errorln("cannot load the service configuration")
		return err
	}

	return generateCert(config.ServerName, c.certPath)
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
