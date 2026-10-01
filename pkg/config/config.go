package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/openkcm/common-sdk/pkg/commoncfg"
	"github.com/spf13/viper"
)

// Config is the root configuration for CmkKryptonBridge.
type Config struct {
	Application   commoncfg.Application `yaml:"application"   mapstructure:"application"`
	Logger        commoncfg.Logger      `yaml:"logger"        mapstructure:"logger"`
	GRPCServer    commoncfg.GRPCServer  `yaml:"grpcServer"    mapstructure:"grpcServer"`
	KryptonClient KryptonClientConfig   `yaml:"kryptonClient" mapstructure:"kryptonClient"`
	Orbital       OrbitalConfig         `yaml:"orbital"       mapstructure:"orbital"`
	KeyHierarchy  KeyHierarchyConfig    `yaml:"keyHierarchy"  mapstructure:"keyHierarchy"`
	Database      DatabaseConfig        `yaml:"database"      mapstructure:"database"`
	Worker        WorkerConfig          `yaml:"worker"        mapstructure:"worker"`
}

// KryptonClientConfig configures the connection to the Krypton backend.
type KryptonClientConfig struct {
	commoncfg.GRPCClient `yaml:",inline" mapstructure:",squash"`
	MockMode             bool `yaml:"mockMode" mapstructure:"mockMode"`
}

// OrbitalConfig configures the orbital sync framework.
type OrbitalConfig struct {
	Enabled       bool   `yaml:"enabled"       mapstructure:"enabled"`
	JWKSEndpoint  string `yaml:"jwksEndpoint"  mapstructure:"jwksEndpoint"`
	MustVerifySig bool   `yaml:"mustVerifySig" mapstructure:"mustVerifySig"`
}

// KeyHierarchyConfig tunes key lifecycle behaviour.
type KeyHierarchyConfig struct {
	MaxDepth int `yaml:"maxDepth" mapstructure:"maxDepth"`
}

// DatabaseConfig holds the PostgreSQL connection string.
type DatabaseConfig struct {
	DSN string `yaml:"dsn" mapstructure:"dsn"`
}

// WorkerConfig controls the background polling worker pool.
type WorkerConfig struct {
	Concurrency  int           `yaml:"concurrency"  mapstructure:"concurrency"`
	PollInterval time.Duration `yaml:"pollInterval" mapstructure:"pollInterval"`
	MaxAttempts  int           `yaml:"maxAttempts"  mapstructure:"maxAttempts"`
	StuckTimeout time.Duration `yaml:"stuckTimeout" mapstructure:"stuckTimeout"`
}

// Load reads configuration from file and environment variables.
// Env vars override file values. Prefix: KRYPTON_PROXY_
func Load(cfgFile string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("KRYPTON_PROXY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetDefault("application.name", "cmk-krypton-bridge")
	v.SetDefault("application.buildInfo.version", "0.1.0")
	v.SetDefault("logger.level", "info")
	v.SetDefault("logger.format", "json")
	v.SetDefault("grpcServer.address", ":9092")
	v.SetDefault("grpcServer.enabled", true)
	v.SetDefault("kryptonClient.mockMode", true)
	v.SetDefault("kryptonClient.address", "localhost:9090")
	v.SetDefault("orbital.enabled", false)
	v.SetDefault("orbital.mustVerifySig", false)
	v.SetDefault("keyHierarchy.maxDepth", 4)
	v.SetDefault("database.dsn", "")
	v.SetDefault("worker.concurrency", 3)
	v.SetDefault("worker.pollInterval", "500ms")
	v.SetDefault("worker.maxAttempts", 10)
	v.SetDefault("worker.stuckTimeout", "5m")

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("reading config file %q: %w", cfgFile, err)
		}
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	return cfg, nil
}
