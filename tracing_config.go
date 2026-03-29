package tracing

import (
	"github.com/jwm1rr0rb10/go-errors"
)

var (
	ErrHostIsEmpty = errors.New("host cannot be empty")
	ErrPortIsEmpty = errors.New("port cannot be empty")
)

const (
	defaultHost = "localhost"
	defaultPort = "4318"
)

// config holds tracing configuration parameters.
type config struct {
	host           string
	port           string
	serviceID      string
	serviceName    string
	serviceVersion string
	envName        string
}

// Validate checks required fields.
func (c *config) Validate() error {
	if c.host == "" {
		return ErrHostIsEmpty
	}
	if c.port == "" {
		return ErrPortIsEmpty
	}
	return nil
}

// ConfigParam is a functional option for configuring tracing.
type ConfigParam func(*config)

func WithHost(host string) ConfigParam {
	return func(c *config) { c.host = host }
}

func WithPort(port string) ConfigParam {
	return func(c *config) { c.port = port }
}

func WithServiceID(id string) ConfigParam {
	return func(c *config) { c.serviceID = id }
}

func WithServiceName(name string) ConfigParam {
	return func(c *config) { c.serviceName = name }
}

func WithServiceVersion(version string) ConfigParam {
	return func(c *config) { c.serviceVersion = version }
}

func WithEnvName(env string) ConfigParam {
	return func(c *config) { c.envName = env }
}
