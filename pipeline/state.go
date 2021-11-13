package pipeline

import (
	"sync"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/logstream"
	"github.com/harness/lite-engine/logstream/filestore"
	"github.com/harness/lite-engine/logstream/remote"
)

var (
	state *State
	once  sync.Once
)

// State stores the pipeline state.
type State struct {
	mu        sync.Mutex
	logConfig api.LogConfig
	tiConfig  api.TIConfig
	secrets   []string
	client    logstream.Client
}

func (s *State) Set(secrets []string, logConfig api.LogConfig, tiConfig api.TIConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets = secrets
	s.logConfig = logConfig
	s.tiConfig = tiConfig
}

func (s *State) GetSecrets() []string {
	return s.secrets
}

func (s *State) GetLogStreamClient() logstream.Client {
	if s.client == nil {
		if s.logConfig.URL != "" {
			s.client = remote.NewHTTPClient(s.logConfig.URL, s.logConfig.AccountID,
				s.logConfig.Token, s.logConfig.IndirectUpload, false)
		} else {
			// TODO (shubham): Fix the relative path
			s.client = filestore.New("/tmp")
		}
	}
	return s.client
}

func GetState() *State {
	once.Do(func() {
		state = &State{
			mu:        sync.Mutex{},
			logConfig: api.LogConfig{},
			tiConfig:  api.TIConfig{},
			secrets:   make([]string, 0),
			client:    nil,
		}
	})
	return state
}
