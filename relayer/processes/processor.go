package processes

import (
	"context"
	"sync"

	"github.com/pkg/errors"

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

	wg := sync.WaitGroup{}
	wg.Add(len(processes))
	for _, process := range processes {
		go func(process ProcessWithOptions) {
			// set process name to the context
			ctx = tracing.WithTracingProcess(ctx, process.Name)
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					p.log.Error(ctx, "Received panic during the process execution", logger.Error(errors.Errorf("%s", r)))
					if !process.IsRestartableOnError {
						p.log.Warn(ctx, "The process is not auto-restartable on error")
						return
					}
					p.log.Info(ctx, "Restarting process after the panic")
					p.startProcessWithRestartOnError(ctx, process)
				}
			}()
			p.startProcessWithRestartOnError(ctx, process)
		}(process)
	}
	wg.Wait()

	return nil
}

func (p *Processor) startProcessWithRestartOnError(ctx context.Context, process ProcessWithOptions) {
	for {
		if err := process.Process.Start(ctx); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			p.log.Error(ctx, "Received unexpected error from the process", logger.Error(err))
			if !process.IsRestartableOnError {
				p.log.Warn(ctx, "The process is not auto-restartable on error")
				break
			}
			p.log.Info(ctx, "Restarting process after the error")
		} else {
			return
		}
	}
}
