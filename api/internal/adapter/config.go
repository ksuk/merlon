package adapter

import (
	"fmt"
	"net/url"
	"os"

	"gopkg.in/yaml.v3"
)

type AdapterConfig struct {
	Type           string                    `yaml:"type"`
	BaseURL        string                    `yaml:"base_url"`
	Auth           AuthConfig                `yaml:"auth"`
	Endpoints      map[string]EndpointConfig `yaml:"endpoints"`
	TimeoutSeconds int                       `yaml:"timeout_seconds"`
}

type AuthConfig struct {
	Type         string `yaml:"type"`
	TokenEnv     string `yaml:"token_env"`
	UsernameEnv  string `yaml:"username_env"`
	PasswordEnv  string `yaml:"password_env"`
	HeaderName   string `yaml:"header_name"`
	HeaderValEnv string `yaml:"header_val_env"`
}

type EndpointConfig struct {
	Method       string            `yaml:"method"`
	Path         string            `yaml:"path"`
	Params       map[string]string `yaml:"params"`
	FieldMapping map[string]string `yaml:"field_mapping"`
	ResponseRoot string            `yaml:"response_root"`
}

func LoadAdapterConfig(path string) (*AdapterConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read adapter config: %w", err)
	}

	var cfg AdapterConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse adapter config: %w", err)
	}

	return &cfg, nil
}

func (c *AdapterConfig) Validate() error {
	if c.Type != "rest" {
		return fmt.Errorf("unsupported adapter type %q, only \"rest\" is supported", c.Type)
	}

	if c.BaseURL == "" {
		return fmt.Errorf("base_url is required")
	}
	if _, err := url.Parse(c.BaseURL); err != nil {
		return fmt.Errorf("invalid base_url: %w", err)
	}

	if err := c.Auth.validate(); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	if len(c.Endpoints) == 0 {
		return fmt.Errorf("at least one endpoint is required")
	}

	for name, ep := range c.Endpoints {
		if err := ep.validate(); err != nil {
			return fmt.Errorf("endpoint %q: %w", name, err)
		}
	}

	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = 30
	}

	return nil
}

func (a *AuthConfig) validate() error {
	switch a.Type {
	case "none", "":
		return nil
	case "bearer":
		if a.TokenEnv == "" {
			return fmt.Errorf("token_env is required for bearer auth")
		}
	case "basic":
		if a.UsernameEnv == "" || a.PasswordEnv == "" {
			return fmt.Errorf("username_env and password_env are required for basic auth")
		}
	case "header":
		if a.HeaderName == "" || a.HeaderValEnv == "" {
			return fmt.Errorf("header_name and header_val_env are required for header auth")
		}
	default:
		return fmt.Errorf("unsupported auth type %q", a.Type)
	}
	return nil
}

func (e *EndpointConfig) validate() error {
	if e.Method == "" {
		return fmt.Errorf("method is required")
	}
	if e.Path == "" {
		return fmt.Errorf("path is required")
	}
	if len(e.FieldMapping) == 0 {
		return fmt.Errorf("at least one field_mapping is required")
	}
	return nil
}
