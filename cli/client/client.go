package client

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/drone-runners/drone-runner-docker/engine"
	"github.com/drone/lite-engine/config"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

type clientCommand struct {
	envfile  string
	runStage bool
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

	client, err := getClient(config.ServerName, config.Client.CaCertFile, config.Client.CertFile, config.Client.KeyFile)
	if err != nil {
		return errors.Wrap(err, "failed to get client")
	}

	if c.runStage {
		return runStage(client, config.Client.Bind)
	} else {
		return checkServerHealth(client, config.Client.Bind)
	}
}

func checkServerHealth(client *http.Client, addr string) error {
	r, err := client.Get(fmt.Sprintf("https://%s/healthz", addr))
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

func runStage(client *http.Client, addr string) error {
	defer func() {
		if _, code, err := postCall(client, fmt.Sprintf("https://%s/destroy", addr), nil); err != nil {
			panic(err)
		} else if code != http.StatusOK {
			panic(fmt.Errorf("destroy call failed"))
		}
	}()

	// Setup stage
	if _, code, err := postCall(client, fmt.Sprintf("https://%s/setup", addr), nil); err != nil {
		return err
	} else if code != http.StatusOK {
		return fmt.Errorf("setup call failed")
	}

	// run step call
	s1 := engine.Step{
		ID:         "step1",
		Command:    []string{"pwd; echo drone; echo hello world > foo; cat foo"},
		Entrypoint: []string{"sh", "-c"},
		Image:      "alpine:3.12",
		Volumes: []*engine.VolumeMount{
			{
				Name: "_workspace",
				Path: "/drone/src",
			},
		},
		WorkingDir: "/drone/src",
	}

	if out, code, err := postCall(client, fmt.Sprintf("https://%s/run", addr), s1); err != nil {
		return err
	} else if code != http.StatusOK {
		return fmt.Errorf("step1 call failed: %d", code)
	} else {
		fmt.Println(string(out))
	}

	s2 := engine.Step{
		ID:         "step2",
		Command:    []string{"pwd; cat foo"},
		Entrypoint: []string{"sh", "-c"},
		Image:      "alpine:3.12",
		Volumes: []*engine.VolumeMount{
			{
				Name: "_workspace",
				Path: "/drone/src",
			},
		},
		WorkingDir: "/drone/src",
	}

	if out, code, err := postCall(client, fmt.Sprintf("https://%s/run", addr), s2); err != nil {
		return err
	} else if code != http.StatusOK {
		return fmt.Errorf("step2 call failed: %d", code)
	} else {
		fmt.Println(string(out))
	}
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

func postCall(client *http.Client, url string, v interface{}) ([]byte, int, error) {
	input, err := json.Marshal(v)
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(input))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	r, err := client.Do(req)
	if err != nil {
		return nil, -1, err
	}
	defer r.Body.Close()

	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, -1, err
	}

	return data, r.StatusCode, nil
}

// Register the server commands.
func Register(app *kingpin.Application) {
	c := new(clientCommand)

	cmd := app.Command("client", "check health of the server").
		Action(c.run)

	cmd.Flag("stage", "Run a stage").
		BoolVar(&c.runStage)

	cmd.Flag("env-file", "environment file").
		Default(".env").
		StringVar(&c.envfile)
}
