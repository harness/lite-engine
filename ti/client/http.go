// Copyright 2021 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cenkalti/backoff"
	types "github.com/harness/lite-engine/ti"
)

var _ Client = (*HTTPClient)(nil)

const (
	dbEndpoint            = "/reports/write?accountId=%s&orgId=%s&projectId=%s&pipelineId=%s&buildId=%s&stageId=%s&stepId=%s&report=%s&repo=%s&sha=%s&commitLink=%s"
	testEndpoint          = "/tests/select?accountId=%s&orgId=%s&projectId=%s&pipelineId=%s&buildId=%s&stageId=%s&stepId=%s&repo=%s&sha=%s&source=%s&target=%s"
	cgEndpoint            = "/tests/uploadcg?accountId=%s&orgId=%s&projectId=%s&pipelineId=%s&buildId=%s&stageId=%s&stepId=%s&repo=%s&sha=%s&source=%s&target=%s&timeMs=%d"
	getTestsTimesEndpoint = "/tests/timedata?accountId=%s&orgId=%s&projectId=%s&pipelineId=%s"
	agentEndpoint         = "/agents/link?accountId=%s&language=%s&os=%s&arch=%s&framework=%s&version=%s&buildenv=%s"

	bodyLimitBytes = 4096
)

// defaultClient is the default http.Client.
var defaultClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// NewHTTPClient returns a new HTTPClient.
func NewHTTPClient(endpoint, token, accountID, orgID, projectID, pipelineID, buildID, stageID, repo, sha, commitLink string, skipverify bool, additionalCertsDir string) *HTTPClient {
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
	} else if additionalCertsDir != "" {
		// If additional certs are specified, we append them to the existing cert chain

		// Use the system certs if possible
		rootCAs, _ := x509.SystemCertPool()
		if rootCAs == nil {
			rootCAs = x509.NewCertPool()
		}

		fmt.Printf("additional certs dir to allow: %s\n", additionalCertsDir)

		files, err := os.ReadDir(additionalCertsDir)
		if err != nil {
			fmt.Printf("could not read directory %s, error: %s", additionalCertsDir, err)
			client.Client = clientWithRootCAs(skipverify, rootCAs)
			return client
		}

		// Go through all certs in this directory and add them to the global certs
		for _, f := range files {
			path := filepath.Join(additionalCertsDir, f.Name())
			fmt.Printf("trying to add certs at: %s to root certs\n", path)
			// Create TLS config using cert PEM
			rootPem, err := os.ReadFile(path)
			if err != nil {
				fmt.Printf("could not read certificate file (%s), error: %s", path, err.Error())
				continue
			}
			// Append certs to the global certs
			ok := rootCAs.AppendCertsFromPEM(rootPem)
			if !ok {
				fmt.Printf("error adding cert (%s) to pool, error: %s", path, err.Error())
				continue
			}
			fmt.Printf("successfully added cert at: %s to root certs", path)
		}
		client.Client = clientWithRootCAs(skipverify, rootCAs)
	}
	return client
}

func clientWithRootCAs(skipverify bool, rootCAs *x509.CertPool) *http.Client {
	// Create the HTTP Client with certs
	config := &tls.Config{
		InsecureSkipVerify: skipverify, //nolint:gosec
	}
	if rootCAs != nil {
		config.RootCAs = rootCAs
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
func (c *HTTPClient) Write(ctx context.Context, stepID, report string, tests []*types.TestCase) error {
	if err := c.validateWriteArgs(stepID, report); err != nil {
		return err
	}
	path := fmt.Sprintf(dbEndpoint, c.AccountID, c.OrgID, c.ProjectID, c.PipelineID, c.BuildID, c.StageID, stepID, report, c.Repo, c.Sha, c.CommitLink)
	_, err := c.do(ctx, c.Endpoint+path, "POST", c.Sha, &tests, nil) //nolint:bodyclose
	return err
}

// DownloadLink returns a list of links where the relevant agent artifacts can be downloaded
func (c *HTTPClient) DownloadLink(ctx context.Context, language, os, arch, framework, version, env string) ([]types.DownloadLink, error) {
	var resp []types.DownloadLink
	if err := c.validateDownloadLinkArgs(language); err != nil {
		return resp, err
	}
	path := fmt.Sprintf(agentEndpoint, c.AccountID, language, os, arch, framework, version, env)
	_, err := c.do(ctx, c.Endpoint+path, "GET", "", nil, &resp) //nolint:bodyclose
	return resp, err
}

// SelectTests returns a list of tests which should be run intelligently
func (c *HTTPClient) SelectTests(ctx context.Context, stepID, source, target string, in *types.SelectTestsReq) (types.SelectTestsResp, error) {
	var resp types.SelectTestsResp
	if err := c.validateSelectTestsArgs(stepID, source, target); err != nil {
		return resp, err
	}
	path := fmt.Sprintf(testEndpoint, c.AccountID, c.OrgID, c.ProjectID, c.PipelineID, c.BuildID, c.StageID, stepID, c.Repo, c.Sha, source, target)
	_, err := c.do(ctx, c.Endpoint+path, "POST", c.Sha, in, &resp) //nolint:bodyclose
	return resp, err
}

// UploadCg uploads avro encoded callgraph to server
func (c *HTTPClient) UploadCg(ctx context.Context, stepID, source, target string, timeMs int64, cg []byte) error {
	if err := c.validateUploadCgArgs(stepID, source, target); err != nil {
		return err
	}
	path := fmt.Sprintf(cgEndpoint, c.AccountID, c.OrgID, c.ProjectID, c.PipelineID, c.BuildID, c.StageID, stepID, c.Repo, c.Sha, source, target, timeMs)
	backoff := createBackoff(45 * 60 * time.Second)
	res, err := c.retry(ctx, c.Endpoint+path, "POST", c.Sha, &cg, nil, false, backoff)
	if res != nil {
		res.Body.Close()
	}
	return err
}

// GetTestTimes gets test timing data
func (c *HTTPClient) GetTestTimes(ctx context.Context, in *types.GetTestTimesReq) (types.GetTestTimesResp, error) {
	var resp types.GetTestTimesResp
	if err := c.validateGetTestTimesArgs(); err != nil {
		return resp, err
	}
	path := fmt.Sprintf(getTestsTimesEndpoint, c.AccountID, c.OrgID, c.ProjectID, c.PipelineID)
	_, err := c.do(ctx, c.Endpoint+path, "POST", "", in, &resp) //nolint:bodyclose
	return resp, err
}

func (c *HTTPClient) retry(ctx context.Context, method, path, sha string, in, out interface{}, isOpen bool, b backoff.BackOff) (*http.Response, error) {
	var res *http.Response
	var err error
	for {
		if !isOpen {
			res, err = c.do(ctx, method, path, sha, in, out)
		} else {
			res, err = c.open(ctx, method, path, in.(io.Reader))
		}

		// do not retry on Canceled or DeadlineExceeded
		if ctxErr := ctx.Err(); ctxErr != nil {
			// Context canceled
			err = ctxErr
			break
		}

		duration := b.NextBackOff()

		if res != nil {
			// Check the response code. We retry on 5xx-range
			// responses to allow the server time to recover, as
			// 5xx's are typically not permanent errors and may
			// relate to outages on the server side.
			if res.StatusCode >= http.StatusInternalServerError {
				// TI server error: Reconnect and retry
				if duration == backoff.Stop {
					res = nil
					break
				}
				time.Sleep(duration)
				continue
			}
		} else if err != nil {
			// Request error: Retry
			if duration == backoff.Stop {
				res = nil
				break
			}
			time.Sleep(duration)
			continue
		}
		break
	}
	if res != nil {
		res.Body.Close()
	}
	return res, err
}

// do is a helper function that posts a signed http request with
// the input encoded and response decoded from json.
func (c *HTTPClient) do(ctx context.Context, path, method, sha string, in, out interface{}) (*http.Response, error) {
	var r io.Reader

	if in != nil {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(in); err != nil {
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
			if _, cerr := io.Copy(io.Discard, io.LimitReader(res.Body, bodyLimitBytes)); cerr != nil {
				fmt.Printf("failed to drain response body with error: %s", cerr)
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
			out := new(Error)
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
