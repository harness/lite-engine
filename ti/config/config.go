package config

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/harness/ti-client/client"
	"github.com/harness/ti-client/types"
)

var (
	ErrStateNotFound = errors.New("no state found")
)

type stepFeature struct {
	feature types.SavingsFeature
	stepID  string
}

type Cfg struct {
	mu              *sync.Mutex
	muruby          *sync.Mutex
	mupy            *sync.Mutex
	rubylocked      int32
	pythonlocked    int32
	client          *client.HTTPClient
	sourceBranch    string
	targetBranch    string
	commitBranch    string
	dataDir         string
	ignoreInstr     bool
	parseSavings    bool
	featureStateMap map[stepFeature]types.IntelligenceExecutionState
}

func New(endpoint, token, accountID, orgID, projectID, pipelineID, buildID, stageID, repo, sha, commitLink,
	sourceBranch, targetBranch, commitBranch, dataDir string, parseSavings, skipVerify bool) Cfg {
	tiClient := client.NewHTTPClient(
		endpoint, token, accountID, orgID, projectID, pipelineID, buildID, stageID, repo, sha, commitLink, skipVerify, "")
	cfg := Cfg{
		mu:              &sync.Mutex{},
		client:          tiClient,
		sourceBranch:    sourceBranch,
		targetBranch:    targetBranch,
		commitBranch:    commitBranch,
		dataDir:         dataDir,
		ignoreInstr:     false,
		parseSavings:    parseSavings,
		featureStateMap: map[stepFeature]types.IntelligenceExecutionState{},
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

func (c *Cfg) GetStageID() string {
	return c.client.StageID
}

func (c *Cfg) GetBuildID() string {
	return c.client.BuildID
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

func (c *Cfg) SetIgnoreInstr(ignoreInstr bool) {
	c.ignoreInstr = ignoreInstr
}

func (c *Cfg) GetIgnoreInstr() bool {
	return c.ignoreInstr
}

func (c *Cfg) GetParseSavings() bool {
	return c.parseSavings
}

func (c *Cfg) WriteFeatureState(stepID string, feature types.SavingsFeature, state types.IntelligenceExecutionState) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.featureStateMap[stepFeature{feature: feature, stepID: stepID}] = state
}

func (c *Cfg) GetFeatureState(stepID string, feature types.SavingsFeature) (types.IntelligenceExecutionState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if state, ok := c.featureStateMap[stepFeature{feature: feature, stepID: stepID}]; ok {
		return state, nil
	}
	return types.DISABLED, ErrStateNotFound
}

func (c *Cfg) LockZipForRuby() {
	c.muruby.Lock()
}
func (c *Cfg) UnlockZipForRuby() {
	c.muruby.Unlock()
}
func (c *Cfg) LockZipForPython() {
	c.mupy.Lock()
}
func (c *Cfg) UnlockZipForPython() {
	c.mupy.Unlock()
}
func (c *Cfg) IsLockedRuby() bool {
	return atomic.LoadInt32(&c.rubylocked) == 1
}
func (c *Cfg) IsLockedPython() bool {
	return atomic.LoadInt32(&c.pythonlocked) == 1
}
