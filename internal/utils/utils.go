package utils

import "fmt"

func GetTrapOutputVarCmd(pairs []string, outputFile string) string {
	cmd := "\ntrap '"
	for _, key := range pairs {
		cmd += fmt.Sprintf("echo \"%s=$%s\" >> %s; ", key, key, outputFile)
	}
	cmd += "' EXIT; "
	return cmd
}

func GetTrapOutputVarCmdFromMap(outputVarsTmp map[string]string, outputFile string) string {
	cmd := "\ntrap '"
	for key, value := range outputVarsTmp {
		cmd += fmt.Sprintf("echo \"%s=%s\" >> %s; ", key, value, outputFile)
	}
	cmd += "' EXIT; "
	return cmd
}
