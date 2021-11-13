package remote

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
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
func NewHTTPClient(endpoint, accountID, token string, indirectUpload, skipverify bool) *HTTPClient {
	client := &HTTPClient{
		Endpoint:       endpoint,
		AccountID:      accountID,
		Token:          token,
		SkipVerify:     skipverify,
		IndirectUpload: indirectUpload,
	}
	if skipverify {
		client.Client = &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}
	}
	return client
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
	resp, err := c.retry(ctx, c.Endpoint+path, "POST", r, nil, true, backoff)
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
	backoff := createBackoff(60 * time.Second)
	_, err := c.retry(ctx, c.Endpoint+path, "POST", nil, out, false, backoff)
	return out, err
}

// uploadUsingLink takes in a reader and a link object and uploads directly to
// remote storage.
func (c *HTTPClient) uploadUsingLink(ctx context.Context, link string, r io.Reader) error {
	backoff := createBackoff(60 * time.Second)
	_, err := c.retry(ctx, link, "PUT", r, nil, true, backoff)
	return err
}

// Open opens the data stream.
func (c *HTTPClient) Open(ctx context.Context, key string) error {
	path := fmt.Sprintf(streamEndpoint, c.AccountID, key)
	backoff := createBackoff(10 * time.Second)
	_, err := c.retry(ctx, c.Endpoint+path, "POST", nil, nil, false, backoff)
	return err
}

// Close closes the data stream.
func (c *HTTPClient) Close(ctx context.Context, key string) error {
	path := fmt.Sprintf(streamEndpoint, c.AccountID, key)
	_, err := c.do(ctx, c.Endpoint+path, "DELETE", nil, nil)
	return err
}

// Write writes logs to the data stream.
func (c *HTTPClient) Write(ctx context.Context, key string, lines []*logstream.Line) error {
	path := fmt.Sprintf(streamEndpoint, c.AccountID, key)
	l := convertLines(lines)
	_, err := c.do(ctx, c.Endpoint+path, "PUT", &l, nil)
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
		if err := ctx.Err(); err != nil {
			logrus.WithError(err).WithField("path", path).Errorln("http: context canceled")
			return res, err
		}

		duration := b.NextBackOff()

		if res != nil {
			// Check the response code. We retry on 5xx-range
			// responses to allow the server time to recover, as
			// 5xx's are typically not permanent errors and may
			// relate to outages on the server side.

			if res.StatusCode >= 500 {
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
		json.NewEncoder(buf).Encode(in)
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
		res = append(res, &Line{
			Level:     l.Level,
			Message:   l.Message,
			Number:    l.Number,
			Timestamp: l.Timestamp,
		})
	}
	return res
}
