package runner

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
)

type counter struct {
	atomic.Int64
}

func newCounter() counter {
	return counter{
		atomic.Int64{},
	}
}

func (c *counter) Start(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		c.Add(1)

		if c.Load()%10 == 0 {
			failureMsg := "failed counter: " + strconv.Itoa(int(c.Load()))
			// Randomly panic or return error.
			if rand.Int()%2 == 0 {
				panic(failureMsg)
			}
			return errors.New(failureMsg)
		}
	}
}

func Test_taskWithRestartOnError(t *testing.T) {
	t.Parallel()

	zapLogger, err := logger.NewZapLogger(logger.ZapLoggerConfig{
		Level:  "error",
		Format: logger.YamlConsoleLoggerFormat,
	})
	require.NoError(t, err)

	tests := []struct {
		name          string
		runTimeout    time.Duration
		retryDelay    time.Duration
		exitOnError   bool
		expectedValue int
		errFunc       require.ErrorAssertionFunc
	}{
		{
			name:          "iterations: 5, exitOnError: false",
			runTimeout:    500 * time.Millisecond,
			retryDelay:    100 * time.Millisecond,
			exitOnError:   false,
			expectedValue: 50,
			errFunc:       require.NoError,
		},
		{
			name:          "iterations: 1, exitOnError: false",
			runTimeout:    50 * time.Millisecond,
			retryDelay:    100 * time.Millisecond,
			exitOnError:   false,
			expectedValue: 10,
			errFunc:       require.NoError,
		},
		{
			name:          "iterations: 5, exitOnError: true",
			runTimeout:    500 * time.Millisecond,
			retryDelay:    100 * time.Millisecond,
			exitOnError:   true,
			expectedValue: 10,
			errFunc:       require.Error,
		},
	}

	for i, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("counter-%v", i), func(tt *testing.T) {
			tt.Parallel()
			ctx, cancelF := context.WithTimeout(context.Background(), tc.runTimeout)
			defer cancelF()

			c := newCounter()

			err = parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
				spawn(tc.name, parallel.Continue, func(ctx context.Context) error {
					tsk := taskWithRestartOnError(
						c.Start,
						zapLogger,
						tc.exitOnError,
						tc.retryDelay,
					)
					return tsk(ctx)
				})
				return nil
			})
			tc.errFunc(tt, err)
			require.EqualValues(tt, tc.expectedValue, c.Load())
		})
	}
}
