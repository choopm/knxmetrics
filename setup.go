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
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/choopm/knxrpc"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/ziflex/lecho/v3"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	api "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
)

var (
	ErrInvalidAuthCredentials = errors.New("invalid auth credentials")
)

// setup runs other setup funcs or error
func (s *Server) setup() error {
	if err := s.setupKNXRPCClient(); err != nil {
		return err
	}
	if err := s.setupOpenTelemetry(); err != nil {
		return err
	}
	if err := s.setupWebserver(); err != nil {
		return err
	}

	return nil
}

// setupKNXClient sets up knxClient
func (s *Server) setupKNXRPCClient() (err error) {
	s.rpcClient, err = knxrpc.NewClient(s.config.KNXRPC)
	if err != nil {
		return err
	}

	return nil
}

// setupOpenTelemetry configures the OpenTelemetry pipeline or error
func (s *Server) setupOpenTelemetry() (err error) {
	s.metricExporter, err = prometheus.New()
	if err != nil {
		return err
	}
	s.meterProvider = metric.NewMeterProvider(metric.WithReader(s.metricExporter))
	meter := s.meterProvider.Meter("github.com/choopm/knxmetrics")

	for i, mapping := range s.config.Mappings {
		mapping.gauge, err = meter.Float64Gauge(mapping.MetricName, api.WithDescription(mapping.MetricDescription))
		if err != nil {
			return fmt.Errorf("mapping %d (%s): %s", i, mapping.MetricName, err)
		}
		kvs := []attribute.KeyValue{}
		for _, kv := range mapping.MetricAttributes {
			kvs = append(kvs, attribute.KeyValue{
				Key:   attribute.Key(kv.Name),
				Value: attribute.StringValue(kv.Value),
			})
		}
		mapping.attributeSet = attribute.NewSet(kvs...)
	}

	return nil
}

// setupWebserver sets up echo webserver or error
func (s *Server) setupWebserver() error {
	// create echo
	s.e = echo.New()
	s.e.HideBanner = true
	s.e.HidePort = true
	s.e.Use(middleware.Recover())

	if s.config.Server.LogRequests {
		s.e.Logger = lecho.From(*s.log)
		s.e.Use(middleware.RequestID())
		s.e.Use(middleware.Logger())
	}

	// construct path
	metricsPath, err := url.JoinPath("/", s.config.Server.Path)
	if err != nil {
		return fmt.Errorf("unable to build metrics.path: %s", err)
	}
	metricsPath = strings.TrimSuffix(metricsPath, "/")

	// bind metrics
	middlewares := []echo.MiddlewareFunc{}
	if s.config.Server.Auth.Enabled {
		middlewares = append(middlewares, middleware.KeyAuthWithConfig(
			middleware.KeyAuthConfig{
				KeyLookup:  "header:" + s.config.Server.Auth.Header,
				AuthScheme: s.config.Server.Auth.Scheme,
				Validator: func(auth string, c echo.Context) (bool, error) {
					err := s.authenticateStaticSecretKey(auth)
					if err != nil {
						return false, err
					}

					return true, nil
				},
			},
		))
	}
	middlewares = append(middlewares, echo.WrapMiddleware(func(next http.Handler) http.Handler {
		return promhttp.Handler()
	}))
	s.e.Group(s.config.Server.Path, middlewares...)

	// bind root redirect
	redirect := func(c echo.Context) error {
		return c.Redirect(http.StatusTemporaryRedirect, metricsPath+"/")
	}
	s.e.GET("", redirect)
	s.e.GET("/", redirect)

	return nil
}

// authenticateStaticSecretKey authenticates a user provided value val
// using a static secret key.
func (s *Server) authenticateStaticSecretKey(val string) error {
	// strip scheme, trim space
	val, _ = strings.CutPrefix(val, s.config.Server.Auth.Scheme+" ")
	val = strings.TrimSpace(val)

	if subtle.ConstantTimeCompare(
		[]byte(val),
		[]byte(s.config.Server.Auth.SecretKey)) != 1 {
		return ErrInvalidAuthCredentials
	}

	return nil
}
