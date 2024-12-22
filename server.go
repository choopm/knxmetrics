/*
Copyright 2024 Christoph Hoopmann

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package knxmetrics

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/choopm/knxrpc/knx/groupaddress/v1"
	"github.com/choopm/knxrpc/knx/groupaddress/v1/v1connect"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/sdk/metric"
	"golang.org/x/sync/errgroup"
)

// Server state struct
type Server struct {
	config *Config
	log    *zerolog.Logger
	ctx    context.Context

	// e stores the echo instance if any
	e *echo.Echo

	// metricExporter stores the otel exporter
	metricExporter metric.Reader
	// meterProvider stores the OpenTelemetry MeterProvider
	meterProvider *metric.MeterProvider

	rpcClient v1connect.GroupAddressServiceClient
}

// New creates a new *Server instance using a provided config
func New(config *Config, logger *zerolog.Logger) (*Server, error) {
	// validate config
	if config == nil {
		return nil, errors.New("missing config")
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config: %s", err)
	}

	// init logger if missing
	if logger == nil {
		l := zerolog.Nop()
		logger = &l
	}

	s := &Server{
		config: config,
		log:    logger,
	}

	return s, nil
}

// Start starts the server using ctx
func (s *Server) Start(ctx context.Context) error {
	s.log.Trace().
		Interface("config", s.config).
		Msg("initializing server")
	if err := s.setup(); err != nil {
		return err
	}

	s.log.Trace().
		Msg("starting server")
	g, ctx := errgroup.WithContext(ctx)
	s.ctx = ctx

	// start bus watcher for group address updates
	g.Go(s.startKNXSubscriber)

	// start webserver
	g.Go(func() error {
		// shutdown hook, register before Start()
		context.AfterFunc(ctx, func() {
			err := s.e.Shutdown(ctx)
			if err != nil {
				_ = s.e.Close() // nolint:errcheck
			}
		})

		// print info after started
		g.Go(func() error {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
				break
			}

			s.log.Info().
				Str("hostport", s.e.ListenerAddr().String()).
				Str("path", s.config.Server.Path).
				Msg("knxmetrics listening on")
			return nil
		})

		err := s.e.Start(net.JoinHostPort(
			s.config.Server.Host,
			strconv.Itoa(s.config.Server.Port),
		))
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}

		return nil
	})

	// ask for all current values
	g.Go(func() error {
		for _, mapping := range s.config.Mappings {
			_, err := s.rpcClient.Publish(s.ctx, connect.NewRequest(&v1.PublishRequest{
				GroupAddress: mapping.KNXGroupAddress,
				Event:        v1.Event_EVENT_READ,
			}))
			if err != nil {
				return fmt.Errorf("unable to initially read values: %s", err)
			}
		}
		return nil
	})

	// wait for all tasks
	s.log.Debug().Msg("server is running")
	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	s.log.Info().Msg("server stopped")

	return nil
}
