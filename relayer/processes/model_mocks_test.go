// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/CoreumFoundation/coreumbridge-xrpl/relayer/processes (interfaces: ContractClient,XRPLAccountTxScanner,XRPLRPCClient,XRPLTxSigner,MetricRegistry)
//
// Generated by this command:
//
//	mockgen -destination=model_mocks_test.go -package=processes_test . ContractClient,XRPLAccountTxScanner,XRPLRPCClient,XRPLTxSigner,MetricRegistry
//

// Package processes_test is a generated GoMock package.
package processes_test

import (
	context "context"
	reflect "reflect"

	coreum "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	xrpl "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
	types "github.com/cosmos/cosmos-sdk/types"
	data "github.com/rubblelabs/ripple/data"
	gomock "go.uber.org/mock/gomock"
)

// MockContractClient is a mock of ContractClient interface.
type MockContractClient struct {
	ctrl     *gomock.Controller
	recorder *MockContractClientMockRecorder
}

// MockContractClientMockRecorder is the mock recorder for MockContractClient.
type MockContractClientMockRecorder struct {
	mock *MockContractClient
}

// NewMockContractClient creates a new mock instance.
func NewMockContractClient(ctrl *gomock.Controller) *MockContractClient {
	mock := &MockContractClient{ctrl: ctrl}
	mock.recorder = &MockContractClientMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockContractClient) EXPECT() *MockContractClientMockRecorder {
	return m.recorder
}

// GetContractConfig mocks base method.
func (m *MockContractClient) GetContractConfig(arg0 context.Context) (coreum.ContractConfig, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetContractConfig", arg0)
	ret0, _ := ret[0].(coreum.ContractConfig)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetContractConfig indicates an expected call of GetContractConfig.
func (mr *MockContractClientMockRecorder) GetContractConfig(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetContractConfig", reflect.TypeOf((*MockContractClient)(nil).GetContractConfig), arg0)
}

// GetPendingOperations mocks base method.
func (m *MockContractClient) GetPendingOperations(arg0 context.Context) ([]coreum.Operation, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPendingOperations", arg0)
	ret0, _ := ret[0].([]coreum.Operation)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetPendingOperations indicates an expected call of GetPendingOperations.
func (mr *MockContractClientMockRecorder) GetPendingOperations(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPendingOperations", reflect.TypeOf((*MockContractClient)(nil).GetPendingOperations), arg0)
}

// IsInitialized mocks base method.
func (m *MockContractClient) IsInitialized() bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsInitialized")
	ret0, _ := ret[0].(bool)
	return ret0
}

// IsInitialized indicates an expected call of IsInitialized.
func (mr *MockContractClientMockRecorder) IsInitialized() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsInitialized", reflect.TypeOf((*MockContractClient)(nil).IsInitialized))
}

// SaveSignature mocks base method.
func (m *MockContractClient) SaveSignature(arg0 context.Context, arg1 types.AccAddress, arg2, arg3 uint32, arg4 string) (*types.TxResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SaveSignature", arg0, arg1, arg2, arg3, arg4)
	ret0, _ := ret[0].(*types.TxResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// SaveSignature indicates an expected call of SaveSignature.
func (mr *MockContractClientMockRecorder) SaveSignature(arg0, arg1, arg2, arg3, arg4 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SaveSignature", reflect.TypeOf((*MockContractClient)(nil).SaveSignature), arg0, arg1, arg2, arg3, arg4)
}

// SendCoreumToXRPLTransferTransactionResultEvidence mocks base method.
func (m *MockContractClient) SendCoreumToXRPLTransferTransactionResultEvidence(arg0 context.Context, arg1 types.AccAddress, arg2 coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence) (*types.TxResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SendCoreumToXRPLTransferTransactionResultEvidence", arg0, arg1, arg2)
	ret0, _ := ret[0].(*types.TxResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// SendCoreumToXRPLTransferTransactionResultEvidence indicates an expected call of SendCoreumToXRPLTransferTransactionResultEvidence.
func (mr *MockContractClientMockRecorder) SendCoreumToXRPLTransferTransactionResultEvidence(arg0, arg1, arg2 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SendCoreumToXRPLTransferTransactionResultEvidence", reflect.TypeOf((*MockContractClient)(nil).SendCoreumToXRPLTransferTransactionResultEvidence), arg0, arg1, arg2)
}

// SendKeysRotationTransactionResultEvidence mocks base method.
func (m *MockContractClient) SendKeysRotationTransactionResultEvidence(arg0 context.Context, arg1 types.AccAddress, arg2 coreum.XRPLTransactionResultKeysRotationEvidence) (*types.TxResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SendKeysRotationTransactionResultEvidence", arg0, arg1, arg2)
	ret0, _ := ret[0].(*types.TxResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// SendKeysRotationTransactionResultEvidence indicates an expected call of SendKeysRotationTransactionResultEvidence.
func (mr *MockContractClientMockRecorder) SendKeysRotationTransactionResultEvidence(arg0, arg1, arg2 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SendKeysRotationTransactionResultEvidence", reflect.TypeOf((*MockContractClient)(nil).SendKeysRotationTransactionResultEvidence), arg0, arg1, arg2)
}

// SendXRPLTicketsAllocationTransactionResultEvidence mocks base method.
func (m *MockContractClient) SendXRPLTicketsAllocationTransactionResultEvidence(arg0 context.Context, arg1 types.AccAddress, arg2 coreum.XRPLTransactionResultTicketsAllocationEvidence) (*types.TxResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SendXRPLTicketsAllocationTransactionResultEvidence", arg0, arg1, arg2)
	ret0, _ := ret[0].(*types.TxResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// SendXRPLTicketsAllocationTransactionResultEvidence indicates an expected call of SendXRPLTicketsAllocationTransactionResultEvidence.
func (mr *MockContractClientMockRecorder) SendXRPLTicketsAllocationTransactionResultEvidence(arg0, arg1, arg2 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SendXRPLTicketsAllocationTransactionResultEvidence", reflect.TypeOf((*MockContractClient)(nil).SendXRPLTicketsAllocationTransactionResultEvidence), arg0, arg1, arg2)
}

// SendXRPLToCoreumTransferEvidence mocks base method.
func (m *MockContractClient) SendXRPLToCoreumTransferEvidence(arg0 context.Context, arg1 types.AccAddress, arg2 coreum.XRPLToCoreumTransferEvidence) (*types.TxResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SendXRPLToCoreumTransferEvidence", arg0, arg1, arg2)
	ret0, _ := ret[0].(*types.TxResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// SendXRPLToCoreumTransferEvidence indicates an expected call of SendXRPLToCoreumTransferEvidence.
func (mr *MockContractClientMockRecorder) SendXRPLToCoreumTransferEvidence(arg0, arg1, arg2 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SendXRPLToCoreumTransferEvidence", reflect.TypeOf((*MockContractClient)(nil).SendXRPLToCoreumTransferEvidence), arg0, arg1, arg2)
}

// SendXRPLTrustSetTransactionResultEvidence mocks base method.
func (m *MockContractClient) SendXRPLTrustSetTransactionResultEvidence(arg0 context.Context, arg1 types.AccAddress, arg2 coreum.XRPLTransactionResultTrustSetEvidence) (*types.TxResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SendXRPLTrustSetTransactionResultEvidence", arg0, arg1, arg2)
	ret0, _ := ret[0].(*types.TxResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// SendXRPLTrustSetTransactionResultEvidence indicates an expected call of SendXRPLTrustSetTransactionResultEvidence.
func (mr *MockContractClientMockRecorder) SendXRPLTrustSetTransactionResultEvidence(arg0, arg1, arg2 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SendXRPLTrustSetTransactionResultEvidence", reflect.TypeOf((*MockContractClient)(nil).SendXRPLTrustSetTransactionResultEvidence), arg0, arg1, arg2)
}

// MockXRPLAccountTxScanner is a mock of XRPLAccountTxScanner interface.
type MockXRPLAccountTxScanner struct {
	ctrl     *gomock.Controller
	recorder *MockXRPLAccountTxScannerMockRecorder
}

// MockXRPLAccountTxScannerMockRecorder is the mock recorder for MockXRPLAccountTxScanner.
type MockXRPLAccountTxScannerMockRecorder struct {
	mock *MockXRPLAccountTxScanner
}

// NewMockXRPLAccountTxScanner creates a new mock instance.
func NewMockXRPLAccountTxScanner(ctrl *gomock.Controller) *MockXRPLAccountTxScanner {
	mock := &MockXRPLAccountTxScanner{ctrl: ctrl}
	mock.recorder = &MockXRPLAccountTxScannerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockXRPLAccountTxScanner) EXPECT() *MockXRPLAccountTxScannerMockRecorder {
	return m.recorder
}

// ScanTxs mocks base method.
func (m *MockXRPLAccountTxScanner) ScanTxs(arg0 context.Context, arg1 chan<- data.TransactionWithMetaData) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ScanTxs", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// ScanTxs indicates an expected call of ScanTxs.
func (mr *MockXRPLAccountTxScannerMockRecorder) ScanTxs(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ScanTxs", reflect.TypeOf((*MockXRPLAccountTxScanner)(nil).ScanTxs), arg0, arg1)
}

// MockXRPLRPCClient is a mock of XRPLRPCClient interface.
type MockXRPLRPCClient struct {
	ctrl     *gomock.Controller
	recorder *MockXRPLRPCClientMockRecorder
}

// MockXRPLRPCClientMockRecorder is the mock recorder for MockXRPLRPCClient.
type MockXRPLRPCClientMockRecorder struct {
	mock *MockXRPLRPCClient
}

// NewMockXRPLRPCClient creates a new mock instance.
func NewMockXRPLRPCClient(ctrl *gomock.Controller) *MockXRPLRPCClient {
	mock := &MockXRPLRPCClient{ctrl: ctrl}
	mock.recorder = &MockXRPLRPCClientMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockXRPLRPCClient) EXPECT() *MockXRPLRPCClientMockRecorder {
	return m.recorder
}

// AccountInfo mocks base method.
func (m *MockXRPLRPCClient) AccountInfo(arg0 context.Context, arg1 data.Account) (xrpl.AccountInfoResult, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AccountInfo", arg0, arg1)
	ret0, _ := ret[0].(xrpl.AccountInfoResult)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// AccountInfo indicates an expected call of AccountInfo.
func (mr *MockXRPLRPCClientMockRecorder) AccountInfo(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AccountInfo", reflect.TypeOf((*MockXRPLRPCClient)(nil).AccountInfo), arg0, arg1)
}

// Submit mocks base method.
func (m *MockXRPLRPCClient) Submit(arg0 context.Context, arg1 data.Transaction) (xrpl.SubmitResult, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Submit", arg0, arg1)
	ret0, _ := ret[0].(xrpl.SubmitResult)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Submit indicates an expected call of Submit.
func (mr *MockXRPLRPCClientMockRecorder) Submit(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Submit", reflect.TypeOf((*MockXRPLRPCClient)(nil).Submit), arg0, arg1)
}

// MockXRPLTxSigner is a mock of XRPLTxSigner interface.
type MockXRPLTxSigner struct {
	ctrl     *gomock.Controller
	recorder *MockXRPLTxSignerMockRecorder
}

// MockXRPLTxSignerMockRecorder is the mock recorder for MockXRPLTxSigner.
type MockXRPLTxSignerMockRecorder struct {
	mock *MockXRPLTxSigner
}

// NewMockXRPLTxSigner creates a new mock instance.
func NewMockXRPLTxSigner(ctrl *gomock.Controller) *MockXRPLTxSigner {
	mock := &MockXRPLTxSigner{ctrl: ctrl}
	mock.recorder = &MockXRPLTxSignerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockXRPLTxSigner) EXPECT() *MockXRPLTxSignerMockRecorder {
	return m.recorder
}

// MultiSign mocks base method.
func (m *MockXRPLTxSigner) MultiSign(arg0 data.MultiSignable, arg1 string) (data.Signer, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "MultiSign", arg0, arg1)
	ret0, _ := ret[0].(data.Signer)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// MultiSign indicates an expected call of MultiSign.
func (mr *MockXRPLTxSignerMockRecorder) MultiSign(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "MultiSign", reflect.TypeOf((*MockXRPLTxSigner)(nil).MultiSign), arg0, arg1)
}

// MockMetricRegistry is a mock of MetricRegistry interface.
type MockMetricRegistry struct {
	ctrl     *gomock.Controller
	recorder *MockMetricRegistryMockRecorder
}

// MockMetricRegistryMockRecorder is the mock recorder for MockMetricRegistry.
type MockMetricRegistryMockRecorder struct {
	mock *MockMetricRegistry
}

// NewMockMetricRegistry creates a new mock instance.
func NewMockMetricRegistry(ctrl *gomock.Controller) *MockMetricRegistry {
	mock := &MockMetricRegistry{ctrl: ctrl}
	mock.recorder = &MockMetricRegistryMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockMetricRegistry) EXPECT() *MockMetricRegistryMockRecorder {
	return m.recorder
}

// SetMaliciousBehaviourKey mocks base method.
func (m *MockMetricRegistry) SetMaliciousBehaviourKey(arg0 string) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetMaliciousBehaviourKey", arg0)
}

// SetMaliciousBehaviourKey indicates an expected call of SetMaliciousBehaviourKey.
func (mr *MockMetricRegistryMockRecorder) SetMaliciousBehaviourKey(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetMaliciousBehaviourKey", reflect.TypeOf((*MockMetricRegistry)(nil).SetMaliciousBehaviourKey), arg0)
}
