package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/harness/lite-engine/ti"
	"github.com/sirupsen/logrus"
)

const (
	dbEndpoint   = "/reports/write?accountId=%s&orgId=%s&projectId=%s&pipelineId=%s&buildId=%s&stageId=%s&stepId=%s&report=%s&repo=%s&sha=%s"
	testEndpoint = "/tests/select?accountId=%s&orgId=%s&projectId=%s&pipelineId=%s&buildId=%s&stageId=%s&stepId=%s&repo=%s&sha=%s&source=%s&target=%s"
	cgEndpoint   = "/tests/uploadcg?accountId=%s&orgId=%s&projectId=%s&pipelineId=%s&buildId=%s&stageId=%s&stepId=%s&repo=%s&sha=%s&source=%s&target=%s&timeMs=%d"
)

var _ Client = (*HTTPClient)(nil)

// defaultClient is the default http.Client.
var defaultClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// NewHTTPClient returns a new HTTPClient.
func NewHTTPClient(endpoint, token, accountID, orgID, projectID, pipelineID, buildID, stageID, repo, sha string,
	skipverify bool) *HTTPClient {
	client := &HTTPClient{
		Endpoint:   endpoint,
		Token:      token,
		AccountID:  accountID,
		OrgID:      orgID,
		ProjectID:  projectID,
		PipelineID: pipelineID,
		BuildID:    buildID,
		StageID:    stageID,
		Repo:       repo,
		Sha:        sha,
		SkipVerify: skipverify,
	}
	if skipverify {
		client.Client = &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, // nolint:gosec
				},
			},
		}
	}
	return client
}

// HTTPClient provides an http service client.
type HTTPClient struct {
	Client     *http.Client
	Endpoint   string // Example: http://localhost:port
	Token      string
	AccountID  string
	OrgID      string
	ProjectID  string
	PipelineID string
	BuildID    string
	StageID    string
	Repo       string
	Sha        string
	SkipVerify bool
}

// Write writes test results to the TI server
func (c *HTTPClient) Write(ctx context.Context, stepID, report string, tests []*ti.TestCase) error {
	path := fmt.Sprintf(dbEndpoint, c.AccountID, c.OrgID, c.ProjectID, c.PipelineID, c.BuildID, c.StageID, stepID, report, c.Repo, c.Sha)
	_, err := c.do(ctx, c.Endpoint+path, "POST", c.Sha, &tests, nil) // nolint:bodyclose
	return err
}

// SelectTests returns a list of tests which should be run intelligently
func (c *HTTPClient) SelectTests(ctx context.Context, stepID, source, target string, in *ti.SelectTestsReq) (ti.SelectTestsResp, error) {
	path := fmt.Sprintf(testEndpoint, c.AccountID, c.OrgID, c.ProjectID, c.PipelineID, c.BuildID, c.StageID, stepID, c.Repo, c.Sha, source, target)
	var resp ti.SelectTestsResp
	_, err := c.do(ctx, c.Endpoint+path, "POST", c.Sha, in, &resp) // nolint:bodyclose
	return resp, err
}

// UploadCg uploads avro encoded callgraph to server
func (c *HTTPClient) UploadCg(ctx context.Context, stepID, source, target string, timeMs int64, cg []byte) error {
	path := fmt.Sprintf(cgEndpoint, c.AccountID, c.OrgID, c.ProjectID, c.PipelineID, c.BuildID, c.StageID, stepID, c.Repo, c.Sha, source, target, timeMs)
	_, err := c.do(ctx, c.Endpoint+path, "POST", c.Sha, &cg, nil) // nolint:bodyclose
	return err
}

// do is a helper function that posts a signed http request with
// the input encoded and response decoded from json.
func (c *HTTPClient) do(ctx context.Context, path, method, sha string, in, out interface{}) (*http.Response, error) { // nolint:unparam
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
	// adding sha as request-id for logging context
	if sha != "" {
		req.Header.Add("X-Request-ID", sha)
	}
	res, err := c.client().Do(req)
	if res != nil {
		defer func() {
			// drain the response body so we can reuse
			// this connection.
			if _, cerr := io.Copy(io.Discard, io.LimitReader(res.Body, 4096)); cerr != nil { // nolint:gomnd
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
	if res.StatusCode == 204 { // nolint:gomnd
		return res, nil
	}

	// else read the response body into a byte slice.
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return res, err
	}

	if res.StatusCode > 299 { // nolint:gomnd
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

// client is a helper function that returns the default client
// if a custom client is not defined.
func (c *HTTPClient) client() *http.Client {
	if c.Client == nil {
		return defaultClient
	}
	return c.Client
}
