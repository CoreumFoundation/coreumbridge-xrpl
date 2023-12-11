package processes

import (
	"context"
	"runtime/debug"

	"github.com/pkg/errors"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/tracing"
)

//go:generate mockgen -destination=process_mocks_test.go -package=processes_test . Process

// Process is single process interface.
type Process interface {
	Init(ctx context.Context) error
	Start(ctx context.Context) error
}

// ProcessWithOptions process with options for the process life-cycle.
type ProcessWithOptions struct {
	Process Process
	// name of the process
	Name string
	// flag to indicated whether the process should be restarted after the failure automatically.
	IsRestartableOnError bool
}

// Processor is the processor responsible for the processes start and recovery.
type Processor struct {
	log logger.Logger
}

// NewProcessor returns new instance of the Processor.
func NewProcessor(log logger.Logger) *Processor {
	return &Processor{
		log: log,
	}
}

// StartProcesses starts process and waits there full execution.
func (p *Processor) StartProcesses(ctx context.Context, processes ...ProcessWithOptions) error {
	for _, process := range processes {
		if err := process.Process.Init(ctx); err != nil {
			return errors.Wrapf(err, "failed to init process, name:%s", process.Name)
		}
	}

	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		for i := range processes {
			process := processes[i]
			spawn(process.Name, parallel.Continue, func(ctx context.Context) error {
				ctx = tracing.WithTracingProcess(ctx, process.Name)
				return p.startProcessWithRestartOnError(ctx, process)
			})
		}
		return nil
	}, parallel.WithGroupLogger(p.log.ParallelLogger(ctx)))
}

func (p *Processor) startProcessWithRestartOnError(ctx context.Context, process ProcessWithOptions) error {
	for {
		// start process and handle the panic
		err := func() (err error) {
			defer func() {
				if p := recover(); p != nil {
					err = errors.Wrapf(
						parallel.ErrPanic{Value: p, Stack: debug.Stack()},
						"handled panic on process:%s", process.Name,
					)
				}
			}()
			return process.Process.Start(ctx)
		}()
		// restart the process is it is restartable
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			p.log.Error(ctx, "Received unexpected error from the process", logger.Error(err))
			if !process.IsRestartableOnError {
				p.log.Warn(ctx, "The process is not auto-restartable on error")
				return err
			}
			p.log.Info(ctx, "Restarting process after the error")
		} else {
			return nil
		}
	}
}
