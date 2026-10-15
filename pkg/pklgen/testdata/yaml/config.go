package config

import "time"

// Config is decoded with a YAML library such as gopkg.in/yaml.v3.
type Config struct {
	ServerName string            `json:"server_name"`
	MaxConns   int               `yaml:"max_conns,omitempty"`
	Interval   time.Duration     `yaml:"interval"`
	Weights    map[int]float32   `yaml:"weights"`
	Extra      map[string]string `yaml:",inline"`
	Ignored    bool              `yaml:"-"`

	Common `yaml:",inline"`
	Meta
	Nested Nested `yaml:",inline"`
}

type Common struct {
	Region string `yaml:"region"`
}

type Meta struct {
	Owner string
}

type Nested struct {
	Depth uint8 `yaml:"depth,flow"`
}

// Plain has a JSON marshaler, which YAML ignores, and a YAML marshaler only
// for Custom.
type Plain struct {
	Value  string `yaml:"value"`
	Custom Custom `yaml:"custom"`
}

func (Plain) MarshalJSON() ([]byte, error) { return nil, nil }

type Custom struct{}

func (Custom) MarshalYAML() (any, error) { return nil, nil }
