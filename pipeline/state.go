// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package pipeline

import (
	"sync"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/logstream"
	"github.com/harness/lite-engine/logstream/filestore"
	"github.com/harness/lite-engine/logstream/remote"
	"github.com/harness/lite-engine/osstats"
	tiCfg "github.com/harness/lite-engine/ti/config"
)

var (
	state *State
	once  sync.Once
)

const (
	SharedVolPath = "/tmp/engine"
	SharedVolName = "_engine"
)

// State stores the pipeline state.
type State struct {
	mu        sync.Mutex
	logConfig api.LogConfig
	tiConfig  tiCfg.Cfg
	secrets   []string

	statsCollector *osstats.StatsCollector
	logClient      logstream.Client
}

func (s *State) Set(secrets []string, logConfig api.LogConfig, tiConfig tiCfg.Cfg, collector *osstats.StatsCollector) { //nolint:gocritic
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets = secrets
	s.logConfig = logConfig
	s.tiConfig = tiConfig
	s.statsCollector = collector
}

func (s *State) GetSecrets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.secrets
}

func (s *State) GetStatsCollector() *osstats.StatsCollector {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.statsCollector
}

func (s *State) GetLogStreamClient() logstream.Client {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.logClient == nil {
		if s.logConfig.URL != "" {
			s.logClient = remote.NewHTTPClient(s.logConfig.URL, s.logConfig.AccountID,
				s.logConfig.Token, s.logConfig.IndirectUpload, false)
		} else {
			s.logClient = filestore.New(SharedVolPath)
		}
	}
	return s.logClient
}

func (s *State) GetTIConfig() *tiCfg.Cfg {
	s.mu.Lock()
	defer s.mu.Unlock()

	return &s.tiConfig
}

func GetState() *State {
	once.Do(func() {
		state = &State{
			mu:             sync.Mutex{},
			logConfig:      api.LogConfig{},
			tiConfig:       tiCfg.Cfg{},
			statsCollector: &osstats.StatsCollector{},
			secrets:        make([]string, 0),
			logClient:      nil,
		}
	})
	return state
}
