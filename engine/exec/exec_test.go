package exec

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/harness/lite-engine/engine/spec"
)

func TestRun(t *testing.T) {
	testCases := []struct {
		name       string
		entrypoint []string
		command    []string
		wantOut    string
		wantErr    bool
	}{
		{
			name:       "Simple echo command",
			entrypoint: []string{"bash", "-c"},
			command:    []string{"echo 'Hello, world!'"},
			wantOut:    "Executing command:\necho 'Hello, world!'\n\nHello, world!\n",
			wantErr:    false,
		},
		{
			name:       "Multi-line command",
			entrypoint: []string{"bash", "-c"},
			command:    []string{"echo 'Line 1' \necho 'Line 2'"},
			wantOut:    "Executing command:\necho 'Line 1' \necho 'Line 2'\n\nLine 1\nLine 2\n",
			wantErr:    false,
		},
		{
			name:       "Invalid command",
			entrypoint: []string{"bash", "-c"},
			command:    []string{"invalid_command"},
			wantOut:    "Executing command:\ninvalid_command\n\nbash: invalid_command: command not found\n",
			wantErr:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			step := &spec.Step{
				Entrypoint: tc.entrypoint,
				Command:    tc.command,
			}
			output := &bytes.Buffer{}
			_, err := Run(context.Background(), step, output, false)

			gotOut := output.String()
			if !strings.Contains(gotOut, tc.wantOut) {
				t.Errorf("expected output to contain:\n%q\ngot:\n%s", tc.wantOut, gotOut)
			}

			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("unexpected error status: got %v, want %v", gotErr, tc.wantErr)
			}
		})
	}
}
