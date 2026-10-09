package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	runnerRuntime "github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/pipeline"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/stretchr/testify/require"
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

func TestIsLauncherEntrypoint(t *testing.T) {
	cases := []struct {
		name string
		ep   []string
		want bool
	}{
		{"launcher form", []string{"plugin", "-kind", "harness", "-sources", "u"}, true},
		{"custom entrypoint", []string{"/bin/sh", "-c", "echo hi"}, false},
		{"plugin without -kind", []string{"plugin", "-name", "x", "y"}, false},
		{"too short", []string{"plugin", "-kind"}, false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLauncherEntrypoint(tc.ep); got != tc.want {
				t.Fatalf("isLauncherEntrypoint(%v) = %v, want %v", tc.ep, got, tc.want)
			}
		})
	}
}

func TestRunStepOutputCaptureEndToEnd(t *testing.T) {
	for _, tc := range []struct {
		name        string
		interpreter string
		command     string
		flag        bool
		fails       bool
	}{
		{"python alias", "python3", "import os\nos.environ.pop('RESULT', None)\nos.environ['SOURCE'] = 'hello'", true, false},
		{"python secret alias", "python3", "import os\nos.environ.pop('RESULT', None)\nos.environ['SOURCE'] = 'hello'", true, false},
		{"shell capture", "sh", "export SOURCE=hello", true, false},
		{"shell set function", "bash", "SOURCE=hello\nset() { :; }", true, false},
		{"shell errexit wrapper", "bash", "SOURCE=hello\nset -e\nset -o posix\ntr() { command tr \"$@\"; false; printf QQ==; }", true, false},
		{"python alias with decoy", "python3", "import os\nos.environ['RESULT'] = 'wrong'\nos.environ['SOURCE'] = 'hello'", true, false},
		{"python missing source", "python3", "import os\nos.environ.pop('SOURCE', None)\nos.environ['RESULT'] = 'wrong'", true, true},
		{"shell failing encoder", "sh", "base64() { printf aGVsbG8=; return 9; }\nexport SOURCE=hello", true, false},
		{"shell failing tr", "sh", "tr() { printf aGVsbG8=; return 9; }\nexport SOURCE=hello", true, false},
		{"shell flag disabled without encoder", "sh", "export SOURCE=hello\nPATH=" + shellCaptureQuote(t.TempDir()), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outputCaptureInterpreter(t, tc.interpreter)
			workDir := t.TempDir()
			t.Setenv("HARNESS_WORKDIR", workDir)
			require.NoError(t, os.MkdirAll(pipeline.GetSharedVolPath(), 0700))
			tiConfig := tiCfg.New("", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", false, "", "")
			run := func(ctx context.Context, step *spec.Step, out io.Writer, _, _ bool) (*runnerRuntime.State, error) {
				args := append(append([]string{}, step.Entrypoint[1:]...), step.Command...)
				cmd := exec.CommandContext(ctx, step.Entrypoint[0], args...) //nolint:gosec // Execute the fixed test step commands through the real run path.
				cmd.Env = os.Environ()
				for k, v := range step.Envs {
					cmd.Env = append(cmd.Env, k+"="+v)
				}
				cmd.Stdout, cmd.Stderr = out, out
				err := cmd.Run()
				return &runnerRuntime.State{Exited: true, ExitCode: cmd.ProcessState.ExitCode()}, err
			}
			outputType := api.OutputTypeString
			if tc.name == "python secret alias" {
				outputType = api.OutputTypeSecret
			}
			request := &api.StartStepRequest{ID: "expose", WorkingDir: workDir,
				Envs:    map[string]string{ciNewVersionGodotEnv: strconv.FormatBool(tc.flag)},
				Run:     api.RunConfig{Entrypoint: []string{tc.interpreter, "-c"}, Command: []string{tc.command}},
				Outputs: []*api.OutputV2{{Key: "RESULT", Value: "SOURCE", Type: outputType}}}
			state, _, _, _, outputs, _, _, err := executeRunStep(context.Background(), run, request, io.Discard, &tiConfig) //nolint:dogsled
			if tc.fails {
				require.Error(t, err)
				require.NotZero(t, state.ExitCode)
				require.Empty(t, outputs)
				return
			}
			require.NoError(t, err)
			require.Zero(t, state.ExitCode)
			require.Len(t, outputs, 1)
			require.Equal(t, outputType, outputs[0].Type)
			require.Equal(t, "RESULT", outputs[0].Key)
			require.Equal(t, "hello", outputs[0].Value)
			request = &api.StartStepRequest{ID: "use", WorkingDir: workDir,
				Envs: map[string]string{"CAPTURED": outputs[0].Value},
				Run:  api.RunConfig{Entrypoint: []string{"sh", "-c"}, Command: []string{"printf '%s' \"$CAPTURED\""}}}
			var consumed bytes.Buffer
			state, _, _, _, _, _, _, err = executeRunStep(context.Background(), run, request, &consumed, &tiConfig) //nolint:dogsled
			require.NoError(t, err)
			require.Zero(t, state.ExitCode)
			require.Equal(t, "hello", consumed.String())
		})
	}
}

func outputCaptureInterpreter(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is needed for the output capture integration test", name)
	}
	return path
}

func shellCaptureQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func TestShellOutputCaptureDoesNotOverwriteSource(t *testing.T) {
	for _, name := range []string{"sh", "bash"} {
		t.Run(name, func(t *testing.T) {
			shell := outputCaptureInterpreter(t, name)
			file := filepath.Join(t.TempDir(), "output.env")
			script := "set -eu\nset -- original 'two words'\nexport SOURCE='first'\nreadonly _harness_ci_output='second'\n" +
				getOutputsCmd([]string{"sh"}, []*api.OutputV2{{Key: "output-with-dash", Value: "SOURCE"}, {Key: "SECOND", Value: "_harness_ci_output"}}, file, true) +
				"\n[ \"$#\" = 2 ] && [ \"$1\" = original ] && [ \"$2\" = 'two words' ] || exit 1\n"
			out, err := exec.CommandContext(context.Background(), shell, "-c", script).CombinedOutput()
			require.NoError(t, err, "%s", out)
			values, err := fetchExportedVarsFromEnvFile(file, io.Discard, true)
			require.NoError(t, err)
			require.Equal(t, map[string]string{"output-with-dash": "first", "SECOND": "second"}, values)
		})
	}
}

func TestShellOutputCaptureWriteFailure(t *testing.T) {
	shell := outputCaptureInterpreter(t, "sh")
	file := filepath.Join(t.TempDir(), "missing", "output.env")
	script := "set -e\nexport SOURCE=hello\n" + getShellOutputVarCmd("RESULT", "SOURCE", file) + "\nprintf SHOULD_NOT_RUN\n"
	out, err := exec.CommandContext(context.Background(), shell, "-c", script).CombinedOutput()
	require.Error(t, err)
	require.NotContains(t, string(out), "SHOULD_NOT_RUN")
	_, err = os.Stat(file)
	require.True(t, os.IsNotExist(err))
}

// Compare observable behavior with the original capture command, including customer
// shell functions and error handling. Run each command in a fresh shell/directory.
func TestShellOutputCaptureCompatibility(t *testing.T) {
	for _, name := range []string{"sh", "bash"} {
		shell := outputCaptureInterpreter(t, name)
		for _, tc := range []struct {
			name, setup, source string
			bashOnly, badFile   bool
		}{
			{"literal", "SOURCE=hello\n", "SOURCE", false, false},
			{"multiline", "SOURCE='first # café\nsecond\n\n'\n", "SOURCE", false, false},
			{"set function", "SOURCE=hello\nset() { :; }\n", "SOURCE", true, false},
			{"set alias", "SOURCE=hello\nshopt -s expand_aliases\nalias set=':'\n", "SOURCE", true, false},
			{"disabled set", "SOURCE=hello\nenable -n set\n", "SOURCE", true, false},
			{"error trap", "SOURCE=hello\nset -eE\ntrap 'printf ERR_HOOK' ERR\n", "SOURCE", true, true},
			{"errexit wrapper", "SOURCE=hello\nset -e\nset -o posix\ntr() { command tr \"$@\"; false; printf QQ==; }\n", "SOURCE", true, false},
			{"subshell depth", "", "BASH_SUBSHELL", true, false},
			{"encoder failure", "SOURCE=hello\nbase64() { printf aGVsbG8=; return 9; }\n", "SOURCE", false, false},
			{"tr failure", "SOURCE=hello\ntr() { printf aGVsbG8=; return 9; }\n", "SOURCE", false, false},
			{"missing utilities", "SOURCE=hello\nPATH=\n", "SOURCE", false, false},
			{"unset under nounset", "unset SOURCE\nset -u\n", "SOURCE", false, false},
		} {
			if tc.bashOnly && name != "bash" {
				continue
			}
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				original := fmt.Sprintf("\nprintf '%%s=__B64__%%s\\n' 'RESULT' \"$(printf '%%s' \"$%s\" | base64 | tr -d '\\n')\" >> output.env", tc.source)
				type result struct {
					exit   int
					output string
					file   string
				}
				run := func(capture string) result {
					dir := t.TempDir()
					path := filepath.Join(dir, "output.env")
					if tc.badFile {
						if err := os.Mkdir(path, 0700); err != nil {
							t.Fatal(err)
						}
					}
					cmd := exec.CommandContext(context.Background(), shell, "-c", tc.setup+capture)
					cmd.Dir = dir
					out, err := cmd.Output()
					if cmd.ProcessState == nil {
						t.Fatalf("could not start shell: %v", err)
					}
					var data []byte
					if !tc.badFile {
						data, err = os.ReadFile(path)
						if err != nil && !os.IsNotExist(err) {
							t.Fatal(err)
						}
					}
					return result{cmd.ProcessState.ExitCode(), string(out), string(data)}
				}
				want := run(original)
				got := run(getShellOutputVarCmd("RESULT", tc.source, "output.env"))
				if got != want {
					t.Fatalf("capture changed behavior:\noriginal: %+v\ncurrent:  %+v", want, got)
				}
			})
		}
	}
}
