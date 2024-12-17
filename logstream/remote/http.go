// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package remote

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/sirupsen/logrus"

	"github.com/harness/lite-engine/logstream"
)

const (
	streamEndpoint     = "/stream?accountID=%s&key=%s"
	blobEndpoint       = "/blob?accountID=%s&key=%s"
	uploadLinkEndpoint = "/blob/link/upload?accountID=%s&key=%s"
)

var _ logstream.Client = (*HTTPClient)(nil)

// defaultClient is the default http.Client.
var defaultClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// NewHTTPClient returns a new HTTPClient.
func NewHTTPClient(endpoint, accountID, token string, indirectUpload, skipverify bool, base64MtlsClientCert, base64MtlsClientCertKey string) *HTTPClient {
	client := &HTTPClient{
		Endpoint:       endpoint,
		AccountID:      accountID,
		Token:          token,
		SkipVerify:     skipverify,
		IndirectUpload: indirectUpload,
	}

	// Load mTLS certificates if available
	mtlsEnabled, mtlsCerts := loadMTLSCerts(base64MtlsClientCert, base64MtlsClientCertKey)

	// Only create HTTP client if needed (mTLS or skipverify)
	if skipverify || mtlsEnabled {
		client.Client = clientWithTLSConfig(skipverify, mtlsEnabled, mtlsCerts)
	}

	return client
}

// loadMTLSCerts determines the source of mTLS certificates based on base64 strings or file paths
func loadMTLSCerts(base64Cert, base64Key string) (bool, tls.Certificate) {
	// Attempt to load from base64 strings
	if base64Cert != "" && base64Key != "" {
		cert, err := loadCertsFromBase64(base64Cert, base64Key)
		if err == nil {
			return true, cert
		}
		fmt.Printf("failed to load mTLS certs from base64, error: %s\n", err)
	}

	// Return false and an empty tls.Certificate if loading fails or inputs are empty
	return false, tls.Certificate{}
}

// loadCertsFromBase64 loads certificates from base64-encoded strings
func loadCertsFromBase64(certBase64, keyBase64 string) (tls.Certificate, error) {
	certBytes, err := base64.StdEncoding.DecodeString(certBase64)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to decode base64 certificate: %w", err)
	}
	keyBytes, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to decode base64 key: %w", err)
	}
	return tls.X509KeyPair(certBytes, keyBytes)
}

// clientWithTLSConfig creates an HTTP client with the provided TLS settings
func clientWithTLSConfig(skipverify bool, mtlsEnabled bool, cert tls.Certificate) *http.Client {
	config := &tls.Config{
		InsecureSkipVerify: skipverify,
	}
	if mtlsEnabled {
		config.Certificates = []tls.Certificate{cert}
	}
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: config,
		},
	}
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	return err == nil && !info.IsDir()
}

// HTTPClient provides an http service client.
type HTTPClient struct {
	Client         *http.Client
	Endpoint       string // Example: http://localhost:port
	Token          string // Per account token to validate against
	AccountID      string
	SkipVerify     bool
	IndirectUpload bool
}

// UploadFile uploads the file directly to data store or via log service
// if indirectUpload is true, logs go through log service instead of using an uploadable link.
func (c *HTTPClient) Upload(ctx context.Context, key string, lines []*logstream.Line) error {
	data := new(bytes.Buffer)
	for _, line := range convertLines(lines) {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(line); err != nil {
			logrus.WithError(err).WithField("key", key).
				Errorln("failed to encode line")
			return err
		}
		data.Write(buf.Bytes())
	}
	if c.IndirectUpload {
		logrus.WithField("key", key).
			Infoln("uploading logs through log service as indirectUpload is specified as true")
		err := c.uploadToRemoteStorage(ctx, key, data)
		if err != nil {
			logrus.WithError(err).WithField("key", key).
				Errorln("failed to upload logs through log service")
			return err
		}
	} else {
		logrus.WithField("key", key).Infoln("calling upload link")
		link, err := c.uploadLink(ctx, key)
		if err != nil {
			logrus.WithError(err).WithField("key", key).
				Errorln("errored while trying to get upload link")
			return err
		}

		logrus.WithField("key", key).Infoln("uploading logs using link")
		err = c.uploadUsingLink(context.Background(), link.Value, data)
		if err != nil {
			logrus.WithError(err).WithField("key", key).
				Errorln("failed to upload using link")
			return err
		}
	}
	return nil
}

// uploadToRemoteStorage uploads the file to remote storage.
func (c *HTTPClient) uploadToRemoteStorage(ctx context.Context, key string, r io.Reader) error {
	path := fmt.Sprintf(blobEndpoint, c.AccountID, key)
	backoff := createInfiniteBackoff()
	childCtx, cancel := context.WithTimeout(ctx, 60*time.Second) //nolint:gomnd
	defer cancel()
	resp, err := c.retry(childCtx, c.Endpoint+path, "POST", r, nil, true, backoff)
	if resp != nil {
		defer resp.Body.Close()
	}
	return err
}

// uploadLink returns a secure link that can be used to
// upload a file to remote storage.
func (c *HTTPClient) uploadLink(ctx context.Context, key string) (*Link, error) {
	path := fmt.Sprintf(uploadLinkEndpoint, c.AccountID, key)
	out := new(Link)
	backoff := createBackoff(60 * time.Second) //nolint:gomnd
	// 10s should be enought to get the upload link
	childCtx, cancel := context.WithTimeout(ctx, 10*time.Second) //nolint:gomnd
	defer cancel()
	_, err := c.retry(childCtx, c.Endpoint+path, "POST", nil, out, false, backoff) //nolint:bodyclose
	return out, err
}

// uploadUsingLink takes in a reader and a link object and uploads directly to
// remote storage.
func (c *HTTPClient) uploadUsingLink(ctx context.Context, link string, r io.Reader) error {
	backoff := createInfiniteBackoff()
	childCtx, cancel := context.WithTimeout(ctx, 60*time.Second) //nolint:gomnd
	defer cancel()
	_, err := c.retry(childCtx, link, "PUT", r, nil, true, backoff) //nolint:bodyclose
	return err
}

// Open opens the data stream.
func (c *HTTPClient) Open(ctx context.Context, key string) error {
	path := fmt.Sprintf(streamEndpoint, c.AccountID, key)
	backoff := createBackoff(10 * time.Second)                                //nolint:gomnd
	_, err := c.retry(ctx, c.Endpoint+path, "POST", nil, nil, false, backoff) //nolint:bodyclose
	return err
}

// Close closes the data stream.
func (c *HTTPClient) Close(ctx context.Context, key string) error {
	path := fmt.Sprintf(streamEndpoint, c.AccountID, key)
	_, err := c.do(ctx, c.Endpoint+path, "DELETE", nil, nil) //nolint:bodyclose
	return err
}

// Write writes logs to the data stream.
func (c *HTTPClient) Write(ctx context.Context, key string, lines []*logstream.Line) error {
	path := fmt.Sprintf(streamEndpoint, c.AccountID, key)
	l := convertLines(lines)
	_, err := c.do(ctx, c.Endpoint+path, "PUT", &l, nil) //nolint:bodyclose
	return err
}

func (c *HTTPClient) retry(ctx context.Context, method, path string, in, out interface{}, isOpen bool, b backoff.BackOff) (*http.Response, error) {
	for {
		var res *http.Response
		var err error
		if !isOpen {
			res, err = c.do(ctx, method, path, in, out)
		} else {
			res, err = c.open(ctx, method, path, in.(io.Reader))
		}

		// do not retry on Canceled or DeadlineExceeded
		if cerr := ctx.Err(); cerr != nil {
			logrus.WithError(cerr).WithField("path", path).Errorln("http: context canceled")
			return res, cerr
		}

		duration := b.NextBackOff()

		if res != nil {
			// Check the response code. We retry on 5xx-range
			// responses to allow the server time to recover, as
			// 5xx's are typically not permanent errors and may
			// relate to outages on the server side.

			if res.StatusCode >= 500 { //nolint:gomnd
				logrus.WithError(err).WithField("path", path).Warnln("http: log-service server error: reconnect and retry")
				if duration == backoff.Stop {
					return nil, err
				}
				time.Sleep(duration)
				continue
			}
		} else if err != nil {
			logrus.WithError(err).WithField("path", path).Warnln("http: request error. Retrying ...")
			if duration == backoff.Stop {
				return nil, err
			}
			time.Sleep(duration)
			continue
		}
		return res, err
	}
}

// do is a helper function that posts a signed http request with
// the input encoded and response decoded from json.
func (c *HTTPClient) do(ctx context.Context, path, method string, in, out interface{}) (*http.Response, error) {
	var r io.Reader

	if in != nil {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(in); err != nil {
			logrus.WithError(err).WithField("in", in).Errorln("failed to encode input")
			return nil, err
		}
		r = buf
	}

	req, err := http.NewRequestWithContext(ctx, method, path, r)
	if err != nil {
		return nil, err
	}

	// the request should include the secret shared between
	// the agent and server for authorization.
	req.Header.Add("X-Harness-Token", c.Token)
	res, err := c.client().Do(req)
	if res != nil {
		defer func() {
			// drain the response body so we can reuse
			// this connection.
			if _, cerr := io.Copy(io.Discard, io.LimitReader(res.Body, 4096)); cerr != nil { //nolint:gomnd
				logrus.WithError(cerr).Errorln("failed to drain response body")
			}
			res.Body.Close()
		}()
	}
	if err != nil {
		return res, err
	}

	// if the response body return no content we exit
	// immediately. We do not read or unmarshal the response
	// and we do not return an error.
	if res.StatusCode == 204 { //nolint:gomnd
		return res, nil
	}

	// else read the response body into a byte slice.
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return res, err
	}

	if res.StatusCode > 299 { //nolint:gomnd
		// if the response body includes an error message
		// we should return the error string.
		if len(body) != 0 {
			out := new(struct {
				Message string `json:"error_msg"`
			})
			if err := json.Unmarshal(body, out); err == nil {
				return res, &Error{Code: res.StatusCode, Message: out.Message}
			}
			return res, &Error{Code: res.StatusCode, Message: string(body)}
		}
		// if the response body is empty we should return
		// the default status code text.
		return res, errors.New(
			http.StatusText(res.StatusCode),
		)
	}
	if out == nil {
		return res, nil
	}
	return res, json.Unmarshal(body, out)
}

// helper function to open an http request
func (c *HTTPClient) open(ctx context.Context, path, method string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Add("X-Harness-Token", c.Token)
	return c.client().Do(req)
}

// client is a helper function that returns the default client
// if a custom client is not defined.
func (c *HTTPClient) client() *http.Client {
	if c.Client == nil {
		return defaultClient
	}
	return c.Client
}

func createInfiniteBackoff() *backoff.ExponentialBackOff {
	return createBackoff(0)
}

func createBackoff(maxElapsedTime time.Duration) *backoff.ExponentialBackOff {
	exp := backoff.NewExponentialBackOff()
	exp.MaxElapsedTime = maxElapsedTime
	return exp
}

func convertLines(lines []*logstream.Line) []*Line {
	var res []*Line
	for _, l := range lines {
		res = append(res, ConvertToRemote(l))
	}
	return res
}
