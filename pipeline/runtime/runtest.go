package runtime

import (
	"context"
	"errors"
	"io"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
)

func executeRunTestStep(ctx context.Context, engine *engine.Engine, r *api.StartStepRequest, out io.Writer) (
	*runtime.State, map[string]string, error) {
	return nil, nil, errors.New("worked")
}
