package pipeline

import (
	"sync"
)

var (
	envState *EnvState
	o        sync.Once
)

// EnvState stores the exported env variables by a step in a stage.
type EnvState struct {
	mu  sync.Mutex
	env map[string]map[string]string
}

func (s *EnvState) Get(stageRuntimeID string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()

	val, ok := s.env[stageRuntimeID]
	if ok {
		return val
	}
	return nil
}

func (s *EnvState) Add(stageRuntimeID string, envs map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.env[stageRuntimeID]; !ok {
		s.env[stageRuntimeID] = make(map[string]string)
	}
	for k, v := range envs {
		s.env[stageRuntimeID][k] = v
	}
}

func (s *EnvState) Delete(stageRuntimeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.env, stageRuntimeID)
}

func GetEnvState() *EnvState {
	o.Do(func() {
		envState = &EnvState{
			mu:  sync.Mutex{},
			env: make(map[string]map[string]string),
		}
	})
	return envState
}
