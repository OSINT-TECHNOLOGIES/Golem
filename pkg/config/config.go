package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Target TargetConfig `yaml:"target"`
	Scan   ScanConfig   `yaml:"scan"`
}

type TargetConfig struct {
	Endpoint  string      `yaml:"endpoint"`
	Auth      string      `yaml:"auth"`
	TokenEnv  string      `yaml:"token_env"`
	Request   RequestConf `yaml:"request"`
	RateLimit string      `yaml:"rate_limit"`
}

type RequestConf struct {
	MessageField string `yaml:"message_field"`
	SessionField string `yaml:"session_field"`
}

type ScanConfig struct {
	Profile string  `yaml:"profile"`
	Budget  float64 `yaml:"budget"`
	Output  string  `yaml:"output"`
}

func Default() *Config {
	return &Config{
		Target: TargetConfig{
			Auth:     "bearer",
			TokenEnv: "OPENAI_API_KEY",
			Request: RequestConf{
				MessageField: "message",
				SessionField: "session",
			},
			RateLimit: "60/min",
		},
		Scan: ScanConfig{
			Profile: "standard",
			Budget:  5.0,
			Output:  "./report",
		},
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
