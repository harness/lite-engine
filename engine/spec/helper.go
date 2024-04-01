package spec

const (
	allowEmptyEnvFlag = "CI_USE_LESS_STRICT_EVALUATION_FOR_MAP_VARS"
)

// helper function that converts a key value map of
// environment variables to a string slice in key=value
// format.
func ToEnv(env map[string]string) []string {
	allowEmpty := false // default
	if env != nil {
		if val, ok := env[allowEmptyEnvFlag]; ok && val == "true" {
			allowEmpty = true
		}
	}
	var envs []string
	for k, v := range env {
		if v == "" && !allowEmpty {
			continue
		}
		envs = append(envs, k+"="+v)
	}
	return envs
}
