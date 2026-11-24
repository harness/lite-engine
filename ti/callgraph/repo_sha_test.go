package callgraph

import (
	"testing"

	"github.com/stretchr/testify/assert"

	tiCfg "github.com/harness/lite-engine/ti/config"
)

// The v2 payload is stamped with the repo and commit of the TI client. The
// client handed out by Cfg is instrumented, so these must be read through
// Cfg rather than by asserting on the concrete client type.
func TestRepoAndSha_AreTakenFromTheConfiguredClient(t *testing.T) {
	cfg := tiCfg.New("http://ti.invalid", "tok", "acc", "org", "proj", "pipe", "build", "stage", "my-repo", "abc123", "", "", "", "", "/tmp", "", false, "", "")

	repo, sha := repoAndSha(&cfg)

	assert.Equal(t, "my-repo", repo)
	assert.Equal(t, "abc123", sha)
}

func TestRepoAndSha_AreEmptyWhenNoClientIsConfigured(t *testing.T) {
	repo, sha := repoAndSha(&tiCfg.Cfg{})

	assert.Empty(t, repo)
	assert.Empty(t, sha)
}
