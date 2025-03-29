// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package certs

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// default key size.
	size        = 2048
	days        = 1080
	serialLimit = 128

	// default organization name for certificates.
	organization = "drone.vm.generated"
)

// Certificate stores a certificate and private key.
type Certificate struct {
	Cert []byte
	Key  []byte
}

type CertList struct {
	CaCertFile string
	CertFile   string
	KeyFile    string
}

// GenerateCert generates a certificate for the host address.
func GenerateCert(host string, ca *Certificate) (*Certificate, error) {
	template, err := newCertificate(organization)
	if err != nil {
		return nil, err
	}
	template.DNSNames = append(template.DNSNames, host)

	tlsCert, err := tls.X509KeyPair(ca.Cert, ca.Key)
	if err != nil {
		return nil, err
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	x509Cert, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return nil, err
	}

	derBytes, err := x509.CreateCertificate(
		rand.Reader, template, x509Cert, &priv.PublicKey, tlsCert.PrivateKey)
	if err != nil {
		return nil, err
	}

	certOut := new(bytes.Buffer)
	certOutErr := pem.Encode(certOut, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: derBytes,
	})
	if certOutErr != nil {
		logrus.
			WithError(certOutErr).
			Errorln("cannot pem encode certOut")
		return nil, certOutErr
	}

	keyOut := new(bytes.Buffer)
	bytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		logrus.WithError(err).Errorln("could not convert key to bytes")
		return nil, err
	}
	keyOutErr := pem.Encode(keyOut, &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: bytes,
	})
	if keyOutErr != nil {
		logrus.
			WithError(keyOutErr).
			Errorln("cannot pem encode keyout")
		return nil, keyOutErr
	}

	return &Certificate{
		Cert: certOut.Bytes(),
		Key:  keyOut.Bytes(),
	}, nil
}

// GenerateCA generates a CA certificate.
func GenerateCA() (*Certificate, error) {
	template, err := newCertificate(organization)
	if err != nil {
		return nil, err
	}

	template.IsCA = true
	template.KeyUsage |= x509.KeyUsageCertSign
	template.KeyUsage |= x509.KeyUsageKeyEncipherment
	template.KeyUsage |= x509.KeyUsageKeyAgreement

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	derBytes, err := x509.CreateCertificate(
		rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return nil, err
	}

	certOut := new(bytes.Buffer)
	certOutErr := pem.Encode(certOut, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: derBytes,
	})
	if certOutErr != nil {
		logrus.
			WithError(certOutErr).
			Errorln("cannot pem encode certOut")
		return nil, certOutErr
	}
	keyOut := new(bytes.Buffer)
	bytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		logrus.WithError(err).Errorln("could not convert key to bytes")
		return nil, err
	}
	keyOutErr := pem.Encode(keyOut, &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: bytes,
	})
	if keyOutErr != nil {
		logrus.
			WithError(keyOutErr).
			Errorln("cannot pem encode keyout")
		return nil, keyOutErr
	}
	return &Certificate{
		Cert: certOut.Bytes(),
		Key:  keyOut.Bytes(),
	}, nil
}

func newCertificate(org string) (*x509.Certificate, error) {
	now := time.Now()
	// need to set notBefore slightly in the past to account for time
	// skew in the VMs otherwise the certs sometimes are not yet valid
	notBefore := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute()-5, 0, 0, time.Local)
	notAfter := notBefore.Add(time.Hour * 24 * days)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), serialLimit)

	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, err
	}

	return &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{org},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyAgreement,
		BasicConstraintsValid: true,
	}, nil
}

func ReadCerts(caCertFileLocation, certFileLocation, certKeyLocation string) (*CertList, error) {
	caCertFile, err := os.ReadFile(caCertFileLocation)
	if err != nil {
		return nil, err
	}
	certFile, err := os.ReadFile(certFileLocation)
	if err != nil {
		return nil, err
	}
	keyFile, err := os.ReadFile(certKeyLocation)
	if err != nil {
		return nil, err
	}
	return &CertList{CaCertFile: string(caCertFile), CertFile: string(certFile), KeyFile: string(keyFile)}, nil
}
