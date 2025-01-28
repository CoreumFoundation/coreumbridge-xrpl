// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl (interfaces: RPCTxProvider,ScannerMetricRegistry)
//
// Generated by this command:
//
//	mockgen -destination=scanner_mocks_test.go -package=xrpl_test . RPCTxProvider,ScannerMetricRegistry
//

// Package xrpl_test is a generated GoMock package.
package xrpl_test

import (
	context "context"
	reflect "reflect"

	xrpl "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
	data "github.com/rubblelabs/ripple/data"
	gomock "go.uber.org/mock/gomock"
)

// MockRPCTxProvider is a mock of RPCTxProvider interface.
type MockRPCTxProvider struct {
	ctrl     *gomock.Controller
	recorder *MockRPCTxProviderMockRecorder
}

// MockRPCTxProviderMockRecorder is the mock recorder for MockRPCTxProvider.
type MockRPCTxProviderMockRecorder struct {
	mock *MockRPCTxProvider
}

// NewMockRPCTxProvider creates a new mock instance.
func NewMockRPCTxProvider(ctrl *gomock.Controller) *MockRPCTxProvider {
	mock := &MockRPCTxProvider{ctrl: ctrl}
	mock.recorder = &MockRPCTxProviderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRPCTxProvider) EXPECT() *MockRPCTxProviderMockRecorder {
	return m.recorder
}

// AccountTx mocks base method.
func (m *MockRPCTxProvider) AccountTx(arg0 context.Context, arg1 data.Account, arg2, arg3 int64, arg4 map[string]interface{}) (xrpl.AccountTxResult, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AccountTx", arg0, arg1, arg2, arg3, arg4)
	ret0, _ := ret[0].(xrpl.AccountTxResult)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// AccountTx indicates an expected call of AccountTx.
func (mr *MockRPCTxProviderMockRecorder) AccountTx(arg0, arg1, arg2, arg3, arg4 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AccountTx", reflect.TypeOf((*MockRPCTxProvider)(nil).AccountTx), arg0, arg1, arg2, arg3, arg4)
}

// LedgerCurrent mocks base method.
func (m *MockRPCTxProvider) LedgerCurrent(arg0 context.Context) (xrpl.LedgerCurrentResult, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LedgerCurrent", arg0)
	ret0, _ := ret[0].(xrpl.LedgerCurrentResult)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// LedgerCurrent indicates an expected call of LedgerCurrent.
func (mr *MockRPCTxProviderMockRecorder) LedgerCurrent(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LedgerCurrent", reflect.TypeOf((*MockRPCTxProvider)(nil).LedgerCurrent), arg0)
}

// MockScannerMetricRegistry is a mock of ScannerMetricRegistry interface.
type MockScannerMetricRegistry struct {
	ctrl     *gomock.Controller
	recorder *MockScannerMetricRegistryMockRecorder
}

// MockScannerMetricRegistryMockRecorder is the mock recorder for MockScannerMetricRegistry.
type MockScannerMetricRegistryMockRecorder struct {
	mock *MockScannerMetricRegistry
}

// NewMockScannerMetricRegistry creates a new mock instance.
func NewMockScannerMetricRegistry(ctrl *gomock.Controller) *MockScannerMetricRegistry {
	mock := &MockScannerMetricRegistry{ctrl: ctrl}
	mock.recorder = &MockScannerMetricRegistryMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockScannerMetricRegistry) EXPECT() *MockScannerMetricRegistryMockRecorder {
	return m.recorder
}

// SetXRPLAccountFullHistoryScanLedgerIndex mocks base method.
func (m *MockScannerMetricRegistry) SetXRPLAccountFullHistoryScanLedgerIndex(arg0 float64) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetXRPLAccountFullHistoryScanLedgerIndex", arg0)
}

// SetXRPLAccountFullHistoryScanLedgerIndex indicates an expected call of SetXRPLAccountFullHistoryScanLedgerIndex.
func (mr *MockScannerMetricRegistryMockRecorder) SetXRPLAccountFullHistoryScanLedgerIndex(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetXRPLAccountFullHistoryScanLedgerIndex", reflect.TypeOf((*MockScannerMetricRegistry)(nil).SetXRPLAccountFullHistoryScanLedgerIndex), arg0)
}

// SetXRPLAccountRecentHistoryScanLedgerIndex mocks base method.
func (m *MockScannerMetricRegistry) SetXRPLAccountRecentHistoryScanLedgerIndex(arg0 float64) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetXRPLAccountRecentHistoryScanLedgerIndex", arg0)
}

// SetXRPLAccountRecentHistoryScanLedgerIndex indicates an expected call of SetXRPLAccountRecentHistoryScanLedgerIndex.
func (mr *MockScannerMetricRegistryMockRecorder) SetXRPLAccountRecentHistoryScanLedgerIndex(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetXRPLAccountRecentHistoryScanLedgerIndex", reflect.TypeOf((*MockScannerMetricRegistry)(nil).SetXRPLAccountRecentHistoryScanLedgerIndex), arg0)
}
