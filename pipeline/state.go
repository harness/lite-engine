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
	"github.com/harness/lite-engine/telemetry"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/sirupsen/logrus"
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

// OSStatsEntry holds the writer, cancel function, and getSummaryData for a single OS stats stream.
// GetSummaryData returns the collected CPU/memory samples, last payload, and disk I/O totals after
// Cancel() has been called; the caller should then write the summary to Writer and only after that
// call Writer.Close().
type OSStatsEntry struct {
	Writer         logstream.Writer
	Cancel         func()
	GetSummaryData func() osstats.StreamerSummary
}

func (s *State) Set(secrets []string, logConfig api.LogConfig, tiConfig tiCfg.Cfg, mtlsConfig spec.MtlsConfig, collector *osstats.StatsCollector) { //nolint:gocritic
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets = secrets
	s.logConfig = logConfig
	s.tiConfig = tiConfig
	s.mtlsConfig = mtlsConfig
	s.statsCollector = collector

	logrus.WithFields(logrus.Fields{
		"dualLoggingEnabled": logConfig.DualLoggingEnabled,
		"logConfigURL":       logConfig.URL,
		"logConfigAccountID": logConfig.AccountID,
		"trimNewLineSuffix":  logConfig.TrimNewLineSuffix,
		"secretsCount":       len(secrets),
	}).Info("pipeline.State.Set: pipeline state initialized with log config")
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
		s.logClient = newLogStreamClient(&s.logConfig, &s.mtlsConfig, telemetry.Default().Recorder())
	}
	return s.logClient
}

// newLogStreamClient builds the log service client for the stage. Calls to the
// remote log service are reported to rec; the local file store is not a remote
// call and is left unwrapped.
func newLogStreamClient(logConfig *api.LogConfig, mtlsConfig *spec.MtlsConfig, rec telemetry.Recorder) logstream.Client {
	if logConfig.URL == "" {
		return filestore.New(GetSharedVolPath())
	}
	remoteClient := remote.NewHTTPClientWithLogScope(logConfig.URL, logConfig.AccountID,
		logConfig.OrgID, logConfig.ProjectID, logConfig.Token, logConfig.IndirectUpload,
		false, mtlsConfig.ClientCert, mtlsConfig.ClientCertKey)
	return logstream.NewInstrumentedClient(remoteClient, rec)
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
