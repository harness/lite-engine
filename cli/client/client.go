package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/harness/lite-engine/config"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

type clientCommand struct {
	envfile string
}

func (c *clientCommand) run(*kingpin.ParseContext) error {
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

	return checkServerHealth(loadedConfig.Client.Bind, loadedConfig.ServerName, loadedConfig.Client.CaCertFile,
		loadedConfig.Client.CertFile, loadedConfig.Client.KeyFile)
}

func checkServerHealth(addr, serverName, caCertFile, certFile, keyFile string) error {
	c, err := getClient(serverName, caCertFile, certFile, keyFile)
	if err != nil {
		return errors.Wrap(err, "failed to get client")
	}
	r, err := c.Get(fmt.Sprintf("https://%s/healthz", addr)) // nolint: noctx // this is only for testing
	if err != nil {
		return errors.Wrap(err, "health check call failed")
	}
	defer r.Body.Close()

	_, err = io.ReadAll(r.Body)
	if err != nil {
		return errors.Wrap(err, "failed to read from server")
	}
	fmt.Printf("%v\n", r.Status)
	return nil
}

func getClient(serverName, caCertFile, tlsCertFile, tlsKeyFile string) (*http.Client, error) {
	tlsCert, err := tls.LoadX509KeyPair(tlsCertFile, tlsKeyFile)
	if err != nil {
		return nil, err
	}
	tlsConfig := &tls.Config{
		ServerName:   serverName,
		Certificates: []tls.Certificate{tlsCert},
		MinVersion:   tls.VersionTLS12,
	}

	// Trusted server certificate.
	caCert, err := os.ReadFile(caCertFile)
	if err != nil {
		log.Fatal(err)
	}

	tlsConfig.RootCAs = x509.NewCertPool()
	tlsConfig.RootCAs.AppendCertsFromPEM(caCert)
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}, nil
}

// Register the server commands.
func Register(app *kingpin.Application) {
	c := new(clientCommand)

	cmd := app.Command("client", "check health of the server").
		Action(c.run)

	cmd.Flag("env-file", "environment file").
		Default(".env").
		StringVar(&c.envfile)
}
