package config

import (
	"github.com/kelseyhightower/envconfig"
)

// Config provides the system configuration.
type Config struct {
	Debug bool `envconfig:"DEBUG"`
	Trace bool `envconfig:"TRACE"`

	Server struct {
		Bind           string `envconfig:"HTTPS_BIND" default:":9079"`
		CertFile       string `envconfig:"SERVER_CERT_FILE" default:"/tmp/certs/server/cert.pem"` // Server certificate PEM file
		KeyFile        string `envconfig:"SERVER_KEY_FILE" default:"/tmp/certs/server/key.pem"`   // Server key PEM file
		ClientCertFile string `envconfig:"CLIENT_CERT_FILE" default:"/tmp/certs/client/cert.pem"` // Trusted client certificate PEM file for client authentication
	}

	Client struct {
		Bind       string `envconfig:"HTTPS_BIND" default:":9079"`
		CertFile   string `envconfig:"CLIENT_CERT_FILE" default:"/tmp/certs/client/cert.pem"` // Client certificate PEM file
		KeyFile    string `envconfig:"CLIENT_KEY_FILE" default:"/tmp/certs/client/key.pem"`   // Client key PEM file
		CaCertFile string `envconfig:"CA_CERT_FILE" default:"/tmp/certs/server/cert.pem"`     // Server certificate PEM file
	}
}

// Load loads the configuration from the environment.
func Load() (Config, error) {
	cfg := Config{}
	err := envconfig.Process("", &cfg)
	return cfg, err
}
