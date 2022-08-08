package executor

import (
	"sync"

	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/pipeline"
	"github.com/harness/lite-engine/pipeline/runtime"
)

var (
	executor *Executor
	once     sync.Once
)

//TODO:xun add mutex
// Executor maps stage runtime ID to the state of the stage
type Executor struct {
	m map[string]*StageData
}

// GetExecutor returns a singleton executor object used throughout the lifecycle
// of the runner
func GetExecutor() *Executor {
	once.Do(func() {
		executor = &Executor{
			m: make(map[string]*StageData),
		}
	})
	return executor
}

// Get returns the stage data if present, otherwise returns nil.
func (e *Executor) Get(s string) *StageData {
	return e.m[s]
}

// Add maps the stage runtime ID to the stage data
func (e *Executor) Add(s string, sd *StageData) {
	e.m[s] = sd
}

// Remove removes the stage runtime ID from the execution list
func (e *Executor) Remove(s string) {
	delete(e.m, s)
}

// StageData stores the engine and the pipeline state corresponding to a
// stage execution
type StageData struct {
	Engine        *engine.Engine
	State         *pipeline.State
	StepExecutors []*runtime.StepExecutor
}
