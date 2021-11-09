package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/harness/lite-engine/api"
)

// Error represents a json-encoded API error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return e.Message
}

func NewHTTPClient(endpoint, serverName, caCertFile, tlsCertFile, tlsKeyFile string) (*HTTPClient, error) {
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
		return nil, err
	}

	tlsConfig.RootCAs = x509.NewCertPool()
	tlsConfig.RootCAs.AppendCertsFromPEM(caCert)
	return &HTTPClient{
		Client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
		Endpoint: endpoint,
	}, nil
}

// HTTPClient provides an http service client.
type HTTPClient struct {
	Client   *http.Client
	Endpoint string
}

// Setup will setup the stage config
func (c *HTTPClient) Setup(ctx context.Context, in *api.SetupRequest) (*api.SetupResponse, error) {
	path := "setup"
	out := new(api.SetupResponse)
	_, err := c.do(ctx, c.Endpoint+path, "POST", in, out)
	return out, err
}

// Destroy will clean up the resources created
func (c *HTTPClient) Destroy(ctx context.Context, in *api.DestroyRequest) (*api.DestroyResponse, error) {
	path := "destroy"
	out := new(api.DestroyResponse)
	_, err := c.do(ctx, c.Endpoint+path, "POST", in, out)
	return out, err
}

func (c *HTTPClient) StartStep(ctx context.Context, in *api.StartStepRequest) (*api.StartStepResponse, error) {
	path := "start_step"
	out := new(api.StartStepResponse)
	_, err := c.do(ctx, c.Endpoint+path, "POST", in, out)
	return out, err
}

func (c *HTTPClient) PollStep(ctx context.Context, in *api.PollStepRequest) (*api.PollStepResponse, error) {
	path := "poll_step"
	out := new(api.PollStepResponse)
	_, err := c.do(ctx, c.Endpoint+path, "POST", in, out)
	return out, err
}

func (c *HTTPClient) Health(ctx context.Context) error {
	path := "healthz"
	_, err := c.do(ctx, c.Endpoint+path, "POST", nil, nil)
	return err
}

// do is a helper function that posts a http request with
// the input encoded and response decoded from json.
func (c *HTTPClient) do(ctx context.Context, path, method string, in, out interface{}) (*http.Response, error) {
	var r io.Reader

	if in != nil {
		buf := new(bytes.Buffer)
		json.NewEncoder(buf).Encode(in)
		r = buf
	}

	req, err := http.NewRequestWithContext(ctx, method, path, r)
	if err != nil {
		return nil, err
	}

	res, err := c.Client.Do(req)
	if res != nil {
		defer func() {
			// drain the response body so we can reuse
			// this connection.
			io.Copy(ioutil.Discard, io.LimitReader(res.Body, 4096))
			res.Body.Close()
		}()
	}
	if err != nil {
		return res, err
	}

	// if the response body return no content we exit
	// immediately. We do not read or unmarshal the response
	// and we do not return an error.
	if res.StatusCode == 204 {
		return res, nil
	}

	// else read the response body into a byte slice.
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return res, err
	}

	if res.StatusCode > 299 {
		// if the response body includes an error message
		// we should return the error string.
		if len(body) != 0 {
			out := new(Error)
			if err := json.Unmarshal(body, out); err != nil {
				return res, out
			}
			return res, errors.New(
				string(body),
			)
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
