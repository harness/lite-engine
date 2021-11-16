package executor

import (
	"errors"

	"github.com/harness/lite-engine/livelog"
)

func getNudges() []livelog.Nudge {
	// <search-term> <resolution> <error-msg>
	return []livelog.Nudge{
		livelog.NewNudge("[Kk]illed", "Increase memory resources for the step", errors.New("out of memory")),
		livelog.NewNudge(".*git.* SSL certificate problem",
			"Set sslVerify to false in CI codebase properties", errors.New("SSL certificate error")),
		livelog.NewNudge("Cannot connect to the Docker daemon",
			"Setup dind if it's not running. If dind is running, privileged should be set to true",
			errors.New("could not connect to the docker daemon")),
	}
}
