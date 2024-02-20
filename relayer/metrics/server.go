package metrics

import (
	"context"
	"net"
	"net/http"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
)

// ServerConfig is metric server config.
type ServerConfig struct {
	ListenAddress string
}

// DefaultServerConfig return default ServerConfig.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		ListenAddress: "localhost:9090",
	}
}

// Server is metric server.
type Server struct {
	cfg      ServerConfig
	registry *Registry
}

// NewServer returns new instance of the Server.
func NewServer(cfg ServerConfig, registry *Registry) *Server {
	return &Server{
		cfg:      cfg,
		registry: registry,
	}
}

// Start starts metric server.
func (s *Server) Start(ctx context.Context) error {
	registry := prometheus.NewRegistry()
	if err := s.registry.Register(registry); err != nil {
		return err
	}

	l, err := net.Listen("tcp", s.cfg.ListenAddress)
	if err != nil {
		return errors.Wrap(err, "metric server listener failed")
	}
	defer l.Close()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.InstrumentMetricHandler(
		registry, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
	))

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
