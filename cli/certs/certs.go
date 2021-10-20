package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/alecthomas/kingpin.v2"
)

type certCommand struct {
	certPath string
}

func generateCert(relPath string) {
	err := os.MkdirAll(relPath, os.ModePerm)
	if err != nil {
		log.Fatalf("Failed to create directory %s: %v", relPath, err)
	}

	certFilePath := filepath.Join(relPath, "cert.pem")
	keyFilePath := filepath.Join(relPath, "key.pem")

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("Failed to generate private key: %v", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		log.Fatalf("Failed to generate serial number: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Drone.io"},
		},
		// DNSNames:  []string{"localhost"},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(time.Hour * 24 * 30),

		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		log.Fatalf("Failed to create certificate: %v", err)
	}

	pemCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	if pemCert == nil {
		log.Fatal("Failed to encode certificate to PEM")
	}
	if err := os.WriteFile(certFilePath, pemCert, 0644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote cert.pem at path %s\n", certFilePath)

	privBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		log.Fatalf("Unable to marshal private key: %v", err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})
	if pemKey == nil {
		log.Fatal("Failed to encode key to PEM")
	}
	if err := os.WriteFile(keyFilePath, pemKey, 0600); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote key.pem at path: %s\n", keyFilePath)
}

func (c *certCommand) run(*kingpin.ParseContext) error {
	serverCert := filepath.Join(c.certPath, "server")
	clientCert := filepath.Join(c.certPath, "client")
	generateCert(serverCert)
	generateCert(clientCert)
	return nil
}

// Register the server commands.
func Register(app *kingpin.Application) {
	c := new(certCommand)

	cmd := app.Command("certs", "generates the TLS certificates for local testing").
		Action(c.run)

	cmd.Flag("certPath", "Directory to generate the TLS certificates").
		Default("/tmp/certs").
		StringVar(&c.certPath)
}
