package observability

import (
	"errors"
	"net"
	"net/http"
)

// MetricsServer exposes one process-local Prometheus registry. Worker and
// scheduler use separate servers because Prometheus registries are not shared
// across processes.
type MetricsServer struct {
	server   *http.Server
	listener net.Listener
	errors   chan error
}

func StartMetricsServer(address string, handler http.Handler) (*MetricsServer, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	server := &MetricsServer{
		server:   &http.Server{Handler: newMetricsServerHandler(handler)},
		listener: listener,
		errors:   make(chan error, 1),
	}
	go func() {
		err := server.server.Serve(listener)
		if !errors.Is(err, http.ErrServerClosed) {
			server.errors <- err
		}
		close(server.errors)
	}()
	return server, nil
}

func newMetricsServerHandler(handler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", handler)
	return mux
}

func (s *MetricsServer) Address() string      { return s.listener.Addr().String() }
func (s *MetricsServer) Errors() <-chan error { return s.errors }
func (s *MetricsServer) Close() error         { return s.server.Close() }
