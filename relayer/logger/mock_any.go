package logger

import "go.uber.org/mock/gomock"

// NewAnyLogMock mocks all log levels a part from error and allow any times' execution.
func NewAnyLogMock(ctrl *gomock.Controller) *MockLogger {
	mock := NewMockLogger(ctrl)
	mock.EXPECT().Debug(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mock.EXPECT().Info(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mock.EXPECT().Warn(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	// we don't expect error, for the error handling set the custom handler

	return mock
}
