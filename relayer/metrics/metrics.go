package metrics

import (
	"context"
	"net"
	"net/http"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
)

// Start starts metric server.
func Start(ctx context.Context, addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "metric server listener failed")
	}
	defer l.Close()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{Handler: mux}

	err = parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		spawn("server", parallel.Exit, func(ctx context.Context) error {
			if err := server.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return errors.Wrap(err, "metric server exited")
			}
			return ctx.Err()
		})
		spawn("close", parallel.Exit, func(ctx context.Context) error {
			<-ctx.Done()
			server.Close()
			return ctx.Err()
		})
		return nil
	})

	if errors.Is(err, context.Canceled) {
		return nil
	}

	return err
}
