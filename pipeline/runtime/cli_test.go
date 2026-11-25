package runtime

import (
	"reflect"
	"testing"

	"github.com/harness/lite-engine/engine/spec"
)

func TestInjectCLIPathForWindowsShell(t *testing.T) {
	tests := []struct {
		name       string
		image      string
		entrypoint []string
		command    []string
		want       []string
	}{
		{
			name:       "powershell",
			image:      "windows-image",
			entrypoint: []string{"pwsh"},
			command:    []string{"harness version"},
			want:       []string{`$env:PATH = 'C:\harness\lite-engine;' + $env:PATH; harness version`},
		},
		{
			name:       "cmd",
			image:      "windows-image",
			entrypoint: []string{"cmd.exe"},
			command:    []string{"harness version"},
			want:       []string{`set "PATH=C:\harness\lite-engine;%PATH%" & harness version`},
		},
		{
			name:       "unsupported shell",
			image:      "windows-image",
			entrypoint: []string{"sh"},
			command:    []string{"harness version"},
			want:       []string{"harness version"},
		},
		{
			name:       "not a container step",
			entrypoint: []string{"pwsh"},
			command:    []string{"harness version"},
			want:       []string{"harness version"},
		},
		{
			name:       "empty command",
			image:      "windows-image",
			entrypoint: []string{"pwsh"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &spec.Step{
				Image:      tt.image,
				Entrypoint: tt.entrypoint,
				Command:    append([]string(nil), tt.command...),
			}
			injectCLIPathForWindowsShell(step)
			if !reflect.DeepEqual(step.Command, tt.want) {
				t.Fatalf("command = %#v, want %#v", step.Command, tt.want)
			}
		})
	}
}

func TestInjectCLIPathForWindowsContainerForOS(t *testing.T) {
	step := &spec.Step{
		Image:      "windows-image",
		Entrypoint: []string{"pwsh"},
		Command:    []string{"harness version"},
	}
	injectCLIPathForWindowsContainerForOS(step, windowsOS)
	want := `$env:PATH = 'C:\harness\lite-engine;' + $env:PATH; harness version`
	if !reflect.DeepEqual(step.Command, []string{want}) {
		t.Fatalf("command = %#v, want %#v", step.Command, []string{want})
	}

	step.Command = []string{"harness version"}
	injectCLIPathForWindowsContainerForOS(step, "linux")
	if !reflect.DeepEqual(step.Command, []string{"harness version"}) {
		t.Fatalf("non-Windows command changed: %#v", step.Command)
	}
}
