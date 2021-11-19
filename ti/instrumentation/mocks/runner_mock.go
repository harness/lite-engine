// Code generated by MockGen. DO NOT EDIT.
// Source: runner.go

// Package instrumentation is a generated GoMock package.
package instrumentation

import (
	context "context"
	gomock "github.com/golang/mock/gomock"
	ti "github.com/harness/lite-engine/ti"
	reflect "reflect"
)

// MockTestRunner is a mock of TestRunner interface
type MockTestRunner struct {
	ctrl     *gomock.Controller
	recorder *MockTestRunnerMockRecorder
}

// MockTestRunnerMockRecorder is the mock recorder for MockTestRunner
type MockTestRunnerMockRecorder struct {
	mock *MockTestRunner
}

// NewMockTestRunner creates a new mock instance
func NewMockTestRunner(ctrl *gomock.Controller) *MockTestRunner {
	mock := &MockTestRunner{ctrl: ctrl}
	mock.recorder = &MockTestRunnerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockTestRunner) EXPECT() *MockTestRunnerMockRecorder {
	return m.recorder
}

// GetCmd mocks base method
func (m *MockTestRunner) GetCmd(ctx context.Context, tests []ti.RunnableTest, userArgs, agentConfigPath string, ignoreInstr, runAll bool) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetCmd", ctx, tests, userArgs, agentConfigPath, ignoreInstr, runAll)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetCmd indicates an expected call of GetCmd
func (mr *MockTestRunnerMockRecorder) GetCmd(ctx, tests, userArgs, agentConfigPath, ignoreInstr, runAll interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetCmd", reflect.TypeOf((*MockTestRunner)(nil).GetCmd), ctx, tests, userArgs, agentConfigPath, ignoreInstr, runAll)
}

// AutoDetectPackages mocks base method
func (m *MockTestRunner) AutoDetectPackages(workspace string) ([]string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AutoDetectPackages", workspace)
	ret0, _ := ret[0].([]string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// AutoDetectPackages indicates an expected call of AutoDetectPackages
func (mr *MockTestRunnerMockRecorder) AutoDetectPackages(workspace interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AutoDetectPackages", reflect.TypeOf((*MockTestRunner)(nil).AutoDetectPackages), workspace)
}
