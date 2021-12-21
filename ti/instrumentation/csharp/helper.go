package csharp

import "strings"

func GetBuildTool(args string) string {
	// TODO: [CI-3167] Either move this out to a new framework option in the UI
	// or detect it from the test arguments
	if strings.Contains(args, "nunit-console") || strings.Contains(args, "nunit3-console") {
		return "nunitconsole"
	}
	return "dotnet"
}
