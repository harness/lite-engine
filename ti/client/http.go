// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

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
	"time"

	"github.com/cenkalti/backoff"
	"github.com/harness/lite-engine/ti"
	"github.com/sirupsen/logrus"
)

const (
	dbEndpoint             = "/reports/write?accountId=%s&orgId=%s&projectId=%s&pipelineId=%s&buildId=%s&stageId=%s&stepId=%s&report=%s&repo=%s&sha=%s&commitLink=%s"
	testEndpoint           = "/tests/select?accountId=%s&orgId=%s&projectId=%s&pipelineId=%s&buildId=%s&stageId=%s&stepId=%s&repo=%s&sha=%s&source=%s&target=%s"
	cgEndpoint             = "/tests/uploadcg?accountId=%s&orgId=%s&projectId=%s&pipelineId=%s&buildId=%s&stageId=%s&stepId=%s&repo=%s&sha=%s&source=%s&target=%s&timeMs=%d"
	getTestsTimesEndpoint  = "/tests/timedata?accountId=%s&orgId=%s&projectId=%s&pipelineId=%s"
	agentEndpoint          = "/agents/link?accountId=%s&language=%s&os=%s&arch=%s&framework=%s"
	serverErrorsStatusCode = 500
)

var _ Client = (*HTTPClient)(nil)

// defaultClient is the default http.Client.
var defaultClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// NewHTTPClient returns a new HTTPClient.
func NewHTTPClient(endpoint, token, accountID, orgID, projectID, pipelineID, buildID, stageID, repo, sha, commitLink string,
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
		CommitLink: commitLink,
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
					InsecureSkipVerify: true, //nolint:gosec
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
	CommitLink string
	SkipVerify bool
}

// Write writes test results to the TI server
func (c *HTTPClient) Write(ctx context.Context, stepID, report string, tests []*ti.TestCase) error {
	if err := c.validateWriteArgs(stepID, report); err != nil {
		return err
	}
	path := fmt.Sprintf(dbEndpoint, c.AccountID, c.OrgID, c.ProjectID, c.PipelineID, c.BuildID, c.StageID, stepID, report, c.Repo, c.Sha, c.CommitLink)
	backoff := createBackoff(10 * 60 * time.Second)
	_, err := c.retry(ctx, c.Endpoint+path, "POST", c.Sha, &tests, nil, false, false, backoff) //nolint:bodyclose
	return err
}

// DownloadLink returns a list of links where the relevant agent artifacts can be downloaded
func (c *HTTPClient) DownloadLink(ctx context.Context, language, os, arch, framework string) ([]ti.DownloadLink, error) {
	var resp []ti.DownloadLink
	if err := c.validateDownloadLinkArgs(language); err != nil {
		return resp, err
	}
	path := fmt.Sprintf(agentEndpoint, c.AccountID, language, os, arch, framework)
	backoff := createBackoff(5 * 60 * time.Second)
	_, err := c.retry(ctx, c.Endpoint+path, "GET", "", nil, &resp, false, true, backoff) //nolint:bodyclose
	return resp, err
}

// SelectTests returns a list of tests which should be run intelligently
func (c *HTTPClient) SelectTests(ctx context.Context, stepID, source, target string, in *ti.SelectTestsReq) (ti.SelectTestsResp, error) {
	var resp ti.SelectTestsResp
	if err := c.validateSelectTestsArgs(stepID, source, target); err != nil {
		return resp, err
	}
	path := fmt.Sprintf(testEndpoint, c.AccountID, c.OrgID, c.ProjectID, c.PipelineID, c.BuildID, c.StageID, stepID, c.Repo, c.Sha, source, target)
	backoff := createBackoff(10 * 60 * time.Second)
	_, err := c.retry(ctx, c.Endpoint+path, "POST", c.Sha, in, &resp, false, false, backoff) //nolint:bodyclose
	return resp, err
}

// UploadCg uploads avro encoded callgraph to server
func (c *HTTPClient) UploadCg(ctx context.Context, stepID, source, target string, timeMs int64, cg []byte) error {
	if err := c.validateUploadCgArgs(stepID, source, target); err != nil {
		return err
	}
	path := fmt.Sprintf(cgEndpoint, c.AccountID, c.OrgID, c.ProjectID, c.PipelineID, c.BuildID, c.StageID, stepID, c.Repo, c.Sha, source, target, timeMs)
	backoff := createBackoff(45 * 60 * time.Second)
	_, err := c.retry(ctx, c.Endpoint+path, "POST", c.Sha, &cg, nil, false, true, backoff) //nolint:bodyclose
	return err
}

// GetTestTimes gets test timing data
func (c *HTTPClient) GetTestTimes(ctx context.Context, in *ti.GetTestTimesReq) (ti.GetTestTimesResp, error) {
	var resp ti.GetTestTimesResp
	if err := c.validateGetTestTimesArgs(); err != nil {
		return resp, err
	}
	path := fmt.Sprintf(getTestsTimesEndpoint, c.AccountID, c.OrgID, c.ProjectID, c.PipelineID)
	backoff := createBackoff(10 * 60 * time.Second)
	_, err := c.retry(ctx, c.Endpoint+path, "POST", "", in, &resp, false, true, backoff) //nolint:bodyclose
	return resp, err
}

func (c *HTTPClient) retry(ctx context.Context, path, method, sha string, in, out interface{}, isOpen, retryOnServerErrors bool, b backoff.BackOff) (*http.Response, error) { //nolint:unparam
	for {
		var res *http.Response
		var err error
		if !isOpen {
			res, err = c.do(ctx, method, path, sha, in, out)
		} else {
			res, err = c.open(ctx, method, path, in.(io.Reader))
		}

		// do not retry on Canceled or DeadlineExceeded
		if errCtx := ctx.Err(); errCtx != nil {
			// Context canceled
			return res, errCtx
		}

		duration := b.NextBackOff()

		if res != nil && retryOnServerErrors {
			// Check the response code. We retry on 5xx-range
			// responses to allow the server time to recover, as
			// 5xx's are typically not permanent errors and may
			// relate to outages on the server side.
			if res.StatusCode >= serverErrorsStatusCode {
				// TI server error: Reconnect and retry
				if duration == backoff.Stop {
					return nil, err
				}
				time.Sleep(duration)
				continue
			}
		} else if err != nil {
			// Request error: Retry
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
func (c *HTTPClient) do(ctx context.Context, method, path, sha string, in, out interface{}) (*http.Response, error) {
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
	if res.StatusCode == http.StatusNoContent {
		return res, nil
	}

	// else read the response body into a byte slice.
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return res, err
	}

	if res.StatusCode >= http.StatusMultipleChoices {
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

// helper function to open an http request
func (c *HTTPClient) open(ctx context.Context, path, method string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Add("X-Harness-Token", c.Token)
	return c.client().Do(req)
}

func createBackoff(maxElapsedTime time.Duration) *backoff.ExponentialBackOff {
	exp := backoff.NewExponentialBackOff()
	exp.MaxElapsedTime = maxElapsedTime
	return exp
}

func (c *HTTPClient) validateTiArgs() error {
	if c.Endpoint == "" {
		return fmt.Errorf("ti endpoint is not set")
	}
	if c.Token == "" {
		return fmt.Errorf("ti token is not set")
	}
	return nil
}

func (c *HTTPClient) validateBasicArgs() error {
	if c.AccountID == "" {
		return fmt.Errorf("accountID is not set")
	}
	if c.OrgID == "" {
		return fmt.Errorf("orgID is not set")
	}
	if c.ProjectID == "" {
		return fmt.Errorf("projectID is not set")
	}
	if c.PipelineID == "" {
		return fmt.Errorf("pipelineID is not set")
	}
	return nil
}

func (c *HTTPClient) validateWriteArgs(stepID, report string) error {
	if err := c.validateTiArgs(); err != nil {
		return err
	}
	if err := c.validateBasicArgs(); err != nil {
		return err
	}
	if c.BuildID == "" {
		return fmt.Errorf("buildID is not set")
	}
	if c.StageID == "" {
		return fmt.Errorf("stageID is not set")
	}
	if stepID == "" {
		return fmt.Errorf("stepID is not set")
	}
	if report == "" {
		return fmt.Errorf("report is not set")
	}
	return nil
}

func (c *HTTPClient) validateDownloadLinkArgs(language string) error {
	if err := c.validateTiArgs(); err != nil {
		return err
	}
	if language == "" {
		return fmt.Errorf("language is not set")
	}
	return nil
}

func (c *HTTPClient) validateSelectTestsArgs(stepID, source, target string) error {
	if err := c.validateTiArgs(); err != nil {
		return err
	}
	if err := c.validateBasicArgs(); err != nil {
		return err
	}
	if c.BuildID == "" {
		return fmt.Errorf("buildID is not set")
	}
	if c.StageID == "" {
		return fmt.Errorf("stageID is not set")
	}
	if stepID == "" {
		return fmt.Errorf("stepID is not set")
	}
	if source == "" {
		return fmt.Errorf("source branch is not set")
	}
	if target == "" {
		return fmt.Errorf("target branch is not set")
	}
	return nil
}

func (c *HTTPClient) validateUploadCgArgs(stepID, source, target string) error {
	if err := c.validateTiArgs(); err != nil {
		return err
	}
	if err := c.validateBasicArgs(); err != nil {
		return err
	}
	if c.BuildID == "" {
		return fmt.Errorf("buildID is not set")
	}
	if c.StageID == "" {
		return fmt.Errorf("stageID is not set")
	}
	if stepID == "" {
		return fmt.Errorf("stepID is not set")
	}
	if source == "" {
		return fmt.Errorf("source branch is not set")
	}
	if target == "" {
		return fmt.Errorf("target branch is not set")
	}
	return nil
}

func (c *HTTPClient) validateGetTestTimesArgs() error {
	if err := c.validateTiArgs(); err != nil {
		return err
	}
	return c.validateBasicArgs()
}
