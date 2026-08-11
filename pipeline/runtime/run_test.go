package runtime

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	runnerRuntime "github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/pipeline"
	tiCfg "github.com/harness/lite-engine/ti/config"
)

func TestExecuteRunStepUsesResolvedEnvsForSavings(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv("HARNESS_WORKDIR", workDir)
	if err := os.MkdirAll(pipeline.GetSharedVolPath(), 0o755); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tiConfig := tiCfg.New(server.URL, "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", false, "", "")
	request := &api.StartStepRequest{
		ID: "step",
		Envs: map[string]string{
			"PLUGIN_CACHE_METRICS_FILE":  "cache-metrics.json",
			"PLUGIN_BUILDER_DRIVER_OPTS": "enabled",
		},
	}

	run := func(_ context.Context, step *spec.Step, _ io.Writer, _, _ bool) (*runnerRuntime.State, error) {
		resolvedPath := step.Envs["PLUGIN_CACHE_METRICS_FILE"]
		expectedPath := filepath.Join(pipeline.GetSharedVolPath(), "step-cache-metrics.json")
		if resolvedPath != expectedPath {
			t.Fatalf("resolved cache metrics path = %q, want %q", resolvedPath, expectedPath)
		}
		if err := os.WriteFile(resolvedPath, []byte(`{"total_layers":3,"cached":2}`), 0o600); err != nil {
			t.Fatal(err)
		}
		return &runnerRuntime.State{Exited: true}, nil
	}

	_, _, _, _, _, telemetry, _, err := executeRunStep(context.Background(), run, request, &bytes.Buffer{}, &tiConfig) //nolint:dogsled
	if err != nil {
		t.Fatal(err)
	}
	if telemetry.DlcMetadata.TotalLayers != 3 || telemetry.DlcMetadata.Cached != 2 {
		t.Fatalf("DLC telemetry = %+v, want total layers 3 and cached layers 2", telemetry.DlcMetadata)
	}
	if got := request.Envs["PLUGIN_CACHE_METRICS_FILE"]; got != "cache-metrics.json" {
		t.Fatalf("request env was mutated: got %q", got)
	}
}
