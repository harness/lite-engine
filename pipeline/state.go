// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package pipeline

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine/spec"
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

	// Lite-engine log streaming
	leLogWriter logstream.Writer
	leLogKey    string

	// OS stats live log streaming - map of key -> entry to support multiple concurrent stages
	osStatsEntries map[string]*OSStatsEntry
}

// OSStatsEntry holds the writer and cancel function for a single OS stats stream.
type OSStatsEntry struct {
	Writer logstream.Writer
	Cancel func()
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

func (s *State) SetLELogWriter(writer logstream.Writer, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leLogWriter = writer
	s.leLogKey = key
}

func (s *State) GetLELogWriter() logstream.Writer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.leLogWriter
}

func (s *State) GetLELogKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.leLogKey
}

// SetOSStatsEntry stores an OS stats entry for the given key.
func (s *State) SetOSStatsEntry(key string, entry *OSStatsEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.osStatsEntries == nil {
		s.osStatsEntries = make(map[string]*OSStatsEntry)
	}
	s.osStatsEntries[key] = entry
}

// GetOSStatsEntry retrieves the OS stats entry for the given key.
func (s *State) GetOSStatsEntry(key string) *OSStatsEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.osStatsEntries == nil {
		return nil
	}
	return s.osStatsEntries[key]
}

// DeleteOSStatsEntry removes the OS stats entry for the given key.
func (s *State) DeleteOSStatsEntry(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.osStatsEntries != nil {
		delete(s.osStatsEntries, key)
	}
}

// GetAllOSStatsKeys returns all currently registered OS stats keys.
func (s *State) GetAllOSStatsKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.osStatsEntries == nil {
		return nil
	}
	keys := make([]string, 0, len(s.osStatsEntries))
	for k := range s.osStatsEntries {
		keys = append(keys, k)
	}
	return keys
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
			leLogWriter:    nil,
			leLogKey:       "",
			osStatsEntries: make(map[string]*OSStatsEntry),
		}
	})
	return state
}
