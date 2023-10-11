package processes_test

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/processes"
)

func TestProcessor_StartProcesses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		processesBuilder func(ctrl *gomock.Controller) []processes.ProcessWithOptions
		logErrorsCount   int
		wantErr          bool
	}{
		{
			name: "singe_positive_process",
			processesBuilder: func(ctrl *gomock.Controller) []processes.ProcessWithOptions {
				processMock := NewMockProcess(ctrl)
				processMock.EXPECT().Init(gomock.Any()).Return(nil)
				processMock.EXPECT().Start(gomock.Any()).Return(nil)
				return []processes.ProcessWithOptions{
					{
						Process:              processMock,
						IsRestartableOnError: false,
					},
				}
			},
		},
		{
			name: "singe_positive_process_with_init_fail",
			processesBuilder: func(ctrl *gomock.Controller) []processes.ProcessWithOptions {
				processMock := NewMockProcess(ctrl)
				processMock.EXPECT().Init(gomock.Any()).Return(errors.New("init is failed"))
				return []processes.ProcessWithOptions{
					{
						Process:              processMock,
						IsRestartableOnError: false,
					},
				}
			},
			wantErr: true,
		},
		{
			name: "multiple_processes_with_one_restartable_on_error",
			processesBuilder: func(ctrl *gomock.Controller) []processes.ProcessWithOptions {
				processMock1 := NewMockProcess(ctrl)
				processMock1.EXPECT().Init(gomock.Any()).Return(nil)
				processMock1.EXPECT().Start(gomock.Any()).Return(nil)

				processMock2 := NewMockProcess(ctrl)
				processMock2.EXPECT().Init(gomock.Any()).Return(nil)
				processMock2.EXPECT().Start(gomock.Any()).Return(nil)
				return []processes.ProcessWithOptions{
					{
						Process:              processMock1,
						IsRestartableOnError: true,
					},
					{
						Process:              processMock2,
						IsRestartableOnError: false,
					},
				}
			},
		},
		{
			name: "singe_process_with_error",
			processesBuilder: func(ctrl *gomock.Controller) []processes.ProcessWithOptions {
				processMock := NewMockProcess(ctrl)
				processMock.EXPECT().Init(gomock.Any()).Return(nil)
				failsCount := 1
				processMock.EXPECT().Start(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
					if failsCount == 0 {
						return nil
					}
					failsCount--
					return errors.Errorf("emulating mock error")
				}).Times(1)
				return []processes.ProcessWithOptions{
					{
						Process:              processMock,
						IsRestartableOnError: false,
					},
				}
			},
			logErrorsCount: 1,
		},
		{
			name: "singe_process_with_error_restartable",
			processesBuilder: func(ctrl *gomock.Controller) []processes.ProcessWithOptions {
				processMock := NewMockProcess(ctrl)
				processMock.EXPECT().Init(gomock.Any()).Return(nil)
				failsCount := 1
				processMock.EXPECT().Start(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
					if failsCount == 0 {
						return nil
					}
					failsCount--
					return errors.Errorf("emulating mock error")
				}).Times(2)
				return []processes.ProcessWithOptions{
					{
						Process:              processMock,
						IsRestartableOnError: true,
					},
				}
			},
			logErrorsCount: 1,
		},
		{
			name: "singe_process_with_context_cancel_error_restartable",
			processesBuilder: func(ctrl *gomock.Controller) []processes.ProcessWithOptions {
				processMock := NewMockProcess(ctrl)
				processMock.EXPECT().Init(gomock.Any()).Return(nil)
				failsCount := 1
				processMock.EXPECT().Start(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
					if failsCount == 0 {
						return nil
					}
					failsCount--
					return context.Canceled
				}).Times(1)
				return []processes.ProcessWithOptions{
					{
						Process:              processMock,
						IsRestartableOnError: true,
					},
				}
			},
		},
		{
			name: "singe_process_with_panic",
			processesBuilder: func(ctrl *gomock.Controller) []processes.ProcessWithOptions {
				processMock := NewMockProcess(ctrl)
				processMock.EXPECT().Init(gomock.Any()).Return(nil)
				failsCount := 1
				processMock.EXPECT().Start(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
					if failsCount == 0 {
						return nil
					}
					failsCount--
					panic("emulating panic")
				}).Times(1)
				return []processes.ProcessWithOptions{
					{
						Process:              processMock,
						IsRestartableOnError: false,
					},
				}
			},
			logErrorsCount: 1,
		},
		{
			name: "singe_process_with_panic_restartable",
			processesBuilder: func(ctrl *gomock.Controller) []processes.ProcessWithOptions {
				processMock := NewMockProcess(ctrl)
				processMock.EXPECT().Init(gomock.Any()).Return(nil)
				failsCount := 1
				processMock.EXPECT().Start(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
					if failsCount == 0 {
						return nil
					}
					failsCount--
					panic("emulating panic")
				}).Times(2)
				return []processes.ProcessWithOptions{
					{
						Process:              processMock,
						IsRestartableOnError: true,
					},
				}
			},
			logErrorsCount: 1,
		},
		{
			name: "multiple_processes_positive_and_with_error_restartable_and_with_panic_restartable",
			processesBuilder: func(ctrl *gomock.Controller) []processes.ProcessWithOptions {
				processMock := NewMockProcess(ctrl)
				processMock.EXPECT().Init(gomock.Any()).Return(nil)
				processMock.EXPECT().Start(gomock.Any()).Return(nil)

				failProcessMock := NewMockProcess(ctrl)
				failProcessMock.EXPECT().Init(gomock.Any()).Return(nil)
				failsCount := 1
				failProcessMock.EXPECT().Start(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
					if failsCount == 0 {
						return nil
					}
					failsCount--
					return errors.Errorf("emulating mock error")
				}).Times(2)

				panicProcessMock := NewMockProcess(ctrl)
				panicProcessMock.EXPECT().Init(gomock.Any()).Return(nil)
				panicFailsCount := 1
				panicProcessMock.EXPECT().Start(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
					if panicFailsCount == 0 {
						return nil
					}
					panicFailsCount--
					panic("emulating panic")
				}).Times(2)

				return []processes.ProcessWithOptions{
					{
						Process:              processMock,
						IsRestartableOnError: true,
					},
					{
						Process:              failProcessMock,
						IsRestartableOnError: true,
					},
					{
						Process:              panicProcessMock,
						IsRestartableOnError: true,
					},
				}
			},
			logErrorsCount: 2,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			logMock := logger.NewAnyLogMock(ctrl)
			if tt.logErrorsCount > 0 {
				logMock.EXPECT().Error(gomock.Any(), gomock.Any(), gomock.Any()).Times(tt.logErrorsCount)
			}
			processor := processes.NewProcessor(logMock)

			ctx := context.Background()
			err := processor.StartProcesses(ctx, tt.processesBuilder(ctrl)...)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
