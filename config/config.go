// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package config

import (
	"github.com/kelseyhightower/envconfig"
)

// Config provides the system configuration.
type Config struct {
	Debug      bool   `envconfig:"DEBUG"`
	Trace      bool   `envconfig:"TRACE"`
	ServerName string `envconfig:"SERVER_NAME" default:"drone"`

	Server struct {
		Bind              string `envconfig:"HTTPS_BIND" default:":3000"`
		CertFile          string `envconfig:"SERVER_CERT_FILE" default:"/tmp/certs/server-cert.pem"` // Server certificate PEM file
		KeyFile           string `envconfig:"SERVER_KEY_FILE" default:"/tmp/certs/server-key.pem"`   // Server key PEM file
		CACertFile        string `envconfig:"CLIENT_CERT_FILE" default:"/tmp/certs/ca-cert.pem"`     // CA certificate file
		SkipPrepareServer bool   `envconfig:"SKIP_PREPARE_SERVER" default:"false"`                   // skip prepare server, install docker / git
		Insecure          bool   `envconfig:"SERVER_INSECURE" default:"false"`                       // run in insecure mode
	}

	Client struct {
		Bind       string `envconfig:"HTTPS_BIND" default:":3000"`
		CertFile   string `envconfig:"CLIENT_CERT_FILE" default:"/tmp/certs/server-cert.pem"` // Server certificate PEM file
		KeyFile    string `envconfig:"CLIENT_KEY_FILE" default:"/tmp/certs/server-key.pem"`   // Server Key PEM file
		CaCertFile string `envconfig:"CA_CERT_FILE" default:"/tmp/certs/ca-cert.pem"`         // CA certificate file
		Insecure   bool   `envconfig:"CLIENT_INSECURE" default:"false"`                       // dont check server certificate
	}
}

// Load loads the configuration from the environment.
func Load() (Config, error) {
	cfg := Config{}
	err := envconfig.Process("", &cfg)
	return cfg, err
}
