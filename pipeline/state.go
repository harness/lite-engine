// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package pipeline

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/harness/lite-engine/engine/spec"

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
	defaultSharedVolPath = "/tmp/engine"
	SharedVolName        = "_engine"
)

// GetSharedVolPath returns the shared volume path, using HARNESS_WORKDIR if set.
func GetSharedVolPath() string {
	if workdir := os.Getenv("HARNESS_WORKDIR"); workdir != "" {
		return filepath.Join(workdir, "tmp", "engine")
	}
	return defaultSharedVolPath
}

// State stores the pipeline state.
type State struct {
	mu         sync.Mutex
	logConfig  api.LogConfig
	tiConfig   tiCfg.Cfg
	mtlsConfig spec.MtlsConfig
	secrets    []string

	statsCollector *osstats.StatsCollector
	logClient      logstream.Client
}

func (s *State) Set(secrets []string, logConfig api.LogConfig, tiConfig tiCfg.Cfg, mtlsConfig spec.MtlsConfig, collector *osstats.StatsCollector) { //nolint:gocritic
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets = secrets
	s.logConfig = logConfig
	s.tiConfig = tiConfig
	s.mtlsConfig = mtlsConfig
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
				s.logConfig.Token, s.logConfig.IndirectUpload, false, s.mtlsConfig.ClientCert, s.mtlsConfig.ClientCertKey)
		} else {
			s.logClient = filestore.New(GetSharedVolPath())
		}
	}
	return s.logClient
}

func (s *State) GetTIConfig() *tiCfg.Cfg {
	s.mu.Lock()
	defer s.mu.Unlock()

	return &s.tiConfig
}

func (s *State) GetLogConfig() *api.LogConfig {
	s.mu.Lock()
	defer s.mu.Unlock()

	return &s.logConfig
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
			mtlsConfig:     spec.MtlsConfig{},
		}
	})
	return state
}
