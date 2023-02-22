package config

import "github.com/harness/lite-engine/ti/client"

type Cfg struct {
	client       *client.HTTPClient
	sourceBranch string
	targetBranch string
	commitBranch string
	dataDir      string
}

func New(endpoint, token, accountID, orgID, projectID, pipelineID, buildID, stageID, repo, sha, commitLink,
	sourceBranch, targetBranch, commitBranch, dataDir string, skipVerify bool) Cfg {
	client := client.NewHTTPClient(
		endpoint, token, accountID, orgID, projectID, pipelineID, buildID, stageID, repo, sha, commitLink, skipVerify, "")
	cfg := Cfg{
		client:       client,
		sourceBranch: sourceBranch,
		targetBranch: targetBranch,
		commitBranch: commitBranch,
		dataDir:      dataDir,
	}
	return cfg
}

func (c *Cfg) GetClient() client.Client {
	return c.client
}

func (c *Cfg) GetURL() string {
	return c.client.Endpoint
}

func (c *Cfg) GetToken() string {
	return c.client.Token
}

func (c *Cfg) GetAccountID() string {
	return c.client.AccountID
}

func (c *Cfg) GetOrgID() string {
	return c.client.OrgID
}

func (c *Cfg) GetProjectID() string {
	return c.client.ProjectID
}

func (c *Cfg) GetPipelineID() string {
	return c.client.PipelineID
}

func (c *Cfg) GetDataDir() string {
	return c.dataDir
}

func (c *Cfg) GetSourceBranch() string {
	return c.sourceBranch
}

func (c *Cfg) GetTargetBranch() string {
	return c.targetBranch
}

func (c *Cfg) GetSha() string {
	return c.client.Sha
}
