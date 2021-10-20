package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/drone/lite-engine/config"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

type clientCommand struct {
	envfile string
}

func (c *clientCommand) run(*kingpin.ParseContext) error {
	godotenv.Load(c.envfile)

	// load the system configuration from the environment.
	config, err := config.Load()
	if err != nil {
		logrus.WithError(err).
			Errorln("cannot load the service configuration")
		return err
	}

	checkServerHealth(config.Client.Bind, config.Client.CaCertFile,
		config.Client.CertFile, config.Client.KeyFile)

	return nil
}

func checkServerHealth(addr, caCertFile, certFile, keyFile string) {
	c := getClient(caCertFile, certFile, keyFile)
	r, err := c.Get(fmt.Sprintf("https://%s/healthz", addr))
	if err != nil {
		log.Fatal(err)
	}
	defer r.Body.Close()

	_, err = io.ReadAll(r.Body)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%v\n", r.Status)
}

func getClient(caCertFile, certFile, keyFile string) *http.Client {
	// Load our client certificate and key.
	clientCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		log.Fatal(err)
	}

	// Trusted server certificate.
	cert, err := os.ReadFile(caCertFile)
	if err != nil {
		log.Fatal(err)
	}
	certPool := x509.NewCertPool()
	if ok := certPool.AppendCertsFromPEM(cert); !ok {
		log.Fatalf("unable to parse cert from %s", caCertFile)
	}

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				ServerName:   "drone",
				RootCAs:      certPool,
				Certificates: []tls.Certificate{clientCert},
			},
		},
	}
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
