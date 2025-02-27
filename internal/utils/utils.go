package utils

import "fmt"

func GetTrapOutputCmd(key, value, outputFile string) string {
	return fmt.Sprintf("\ntrap 'echo \"%s=$%s\" >> %s' EXIT; ", key, value, outputFile)
}
