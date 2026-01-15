// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package filestore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"sync"

	"github.com/drone/drone-go/drone"
	"github.com/sirupsen/logrus"

	"github.com/harness/lite-engine/logstream"
)

func New(relPath string) *FileStore {
	return &FileStore{
		mu:      sync.Mutex{},
		relPath: relPath,
		state:   make(map[string]State),
	}
}

type State struct {
	completed bool
	file      *os.File
}

// FileStore provides a file store client.
type FileStore struct {
	mu      sync.Mutex
	relPath string
	state   map[string]State
}

func (f *FileStore) Upload(_ context.Context, key string, lines []*logstream.Line) error {
	return nil
}

func (f *FileStore) UploadRaw(_ context.Context, key string, r io.Reader) error {
	file, err := os.Create(path.Join(f.relPath, key))
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := io.Copy(file, r); err != nil {
		return err
	}
	return file.Sync()
}

// Open opens the data stream.
func (f *FileStore) Open(_ context.Context, key string) error {
	file, err := os.Create(path.Join(f.relPath, key))
	if err != nil {
		return err
	}

	f.mu.Lock()
	f.state[key] = State{file: file}
	f.mu.Unlock()
	return nil
}

// Close closes the data stream.
func (f *FileStore) Close(_ context.Context, key string, snapshot bool) error {
	file, err := f.getFileRef(key)
	if err != nil {
		return err
	}

	err = file.Close()
	f.mu.Lock()
	f.state[key] = State{completed: true, file: f.state[key].file}
	f.mu.Unlock()
	return err
}

// Write writes logs to the file.
func (f *FileStore) Write(_ context.Context, key string, lines []*logstream.Line) error {
	data := new(bytes.Buffer)
	for _, line := range convertLines(lines) {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(line); err != nil {
			logrus.WithError(err).WithField("key", key).
				Errorln("failed to encode line")
			return err
		}
		data.Write(buf.Bytes())
	}

	file, err := f.getFileRef(key)
	if err != nil {
		return err
	}

	if _, err = file.Write(data.Bytes()); err != nil {
		return err
	}
	return file.Sync()
}

func (f *FileStore) getFileRef(key string) (*os.File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.state[key]
	if !ok {
		return nil, errors.New("file is not opened")
	}

	return s.file, nil
}

func convertLines(lines []*logstream.Line) []*drone.Line {
	var res []*drone.Line
	for _, l := range lines {
		res = append(res, &drone.Line{
			Message:   l.Message,
			Number:    l.Number,
			Timestamp: l.ElaspedTime,
		})
	}
	return res
}
