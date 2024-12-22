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
	"fmt"

	"github.com/choopm/knxrpc"
	"github.com/choopm/stdfx/loggingfx"
	"go.opentelemetry.io/otel/attribute"
	api "go.opentelemetry.io/otel/metric"
)

// Config struct stores all config data.
type Config struct {
	// Log stores logging config
	Log loggingfx.Config `mapstructure:"log"`

	// KNXRPC is the knxrpc client config, required
	KNXRPC knxrpc.ClientConfig `mapstructure:"knxrpc"`

	// Server config to use
	Server ServerConfig `mapstructure:"server"`

	// Mappings define the KNX<>metrics mappings
	Mappings []*Mapping `mapstructure:"mappings" default:"[]"`
}

// Validate validates the Config
func (c *Config) Validate() error {
	if err := c.KNXRPC.Validate(); err != nil {
		return err
	}
	if err := c.Server.Validate(); err != nil {
		return err
	}
	if len(c.Mappings) == 0 {
		return fmt.Errorf("missing mappings")
	}
	for i, mapping := range c.Mappings {
		if err := mapping.Validate(); err != nil {
			return fmt.Errorf("mapping %d (%s): %s", i, mapping.MetricName, err)
		}
	}

	return nil
}

// ServerConfig struct stores server config
type ServerConfig struct {
	// Host is the listening host to use when starting a server
	Host string `mapstructure:"host" default:"127.0.0.1"`

	// Port is the listening port to use when starting a server
	Port int `mapstructure:"port" default:"8080"`

	// Path to serve metrics on
	Path string `mapstructure:"path" default:"/metrics"`

	// LogRequests whether to log requests
	LogRequests bool `mapstructure:"logRequests"`

	// Auth config to use
	Auth knxrpc.AuthConfig `mapstructure:"auth"`
}

// Validate validates the ServerConfig
func (c *ServerConfig) Validate() error {
	if len(c.Host) == 0 {
		return fmt.Errorf("missing server.host")
	}
	if c.Port == 0 {
		return fmt.Errorf("missing server.port")
	}
	if len(c.Path) == 0 {
		return fmt.Errorf("missing server.path")
	}
	if err := c.Auth.Validate(); err != nil {
		return err
	}

	return nil
}

// Mapping is used to map KNX GroupAddresses to Metrics
type Mapping struct {
	MetricName        string             `mapstructure:"metricName"`
	MetricType        int                `mapstructure:"metricType"`
	MetricDescription string             `mapstructure:"metricDescription"`
	MetricAttributes  []*MetricAttribute `mapstructure:"metricAttributes" default:"[]"`

	KNXGroupAddress string `mapstructure:"knxGroupAddress"`

	gauge        api.Float64Gauge
	attributeSet attribute.Set
}

// Validate validates the config
func (c *Mapping) Validate() error {
	if len(c.MetricName) == 0 {
		return fmt.Errorf("missing metricName")
	}
	if c.MetricType == 0 {
		return fmt.Errorf("missing metricType")
	}
	if len(c.KNXGroupAddress) == 0 {
		return fmt.Errorf("missing knxGroupAddress")
	}

	return nil
}

// MetricAttribute is used to add attributes to metrics
type MetricAttribute struct {
	Name  string `mapstructure:"name"`
	Value string `mapstructure:"value"`
}
