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
	Registry      RegistryConfig        `yaml:"registry"      mapstructure:"registry"`
}

// KryptonClientConfig configures the connection to the Krypton backend.
type KryptonClientConfig struct {
	commoncfg.GRPCClient `yaml:",inline" mapstructure:",squash"`
	MockMode             bool `yaml:"mockMode" mapstructure:"mockMode"`
}

// OrbitalConfig configures the orbital AMQP operator.
type OrbitalConfig struct {
	Enabled bool              `yaml:"enabled" mapstructure:"enabled"`
	AMQP    OrbitalAMQPConfig `yaml:"amqp"    mapstructure:"amqp"`
}

// OrbitalAMQPConfig holds AMQP broker connection details for the orbital Operator.
type OrbitalAMQPConfig struct {
	URL      string                 `yaml:"url"      mapstructure:"url"`      // e.g. amqps://kms-rabbitmqcluster.services.svc.cluster.local:5671
	Target   string                 `yaml:"target"   mapstructure:"target"`   // exchange to send responses to (CMK listens here)
	Source   string                 `yaml:"source"   mapstructure:"source"`   // queue Bridge listens on for incoming tasks
	Auth     OrbitalAMQPAuthConfig  `yaml:"auth"     mapstructure:"auth"`
}

// OrbitalAMQPAuthConfig selects the auth method for AMQP.
type OrbitalAMQPAuthConfig struct {
	Type  string             `yaml:"type"  mapstructure:"type"`  // "mtls", "basic", or "anonymous"
	MTLS  OrbitalMTLSConfig  `yaml:"mtls"  mapstructure:"mtls"`
	Basic OrbitalBasicConfig `yaml:"basic" mapstructure:"basic"`
}

// OrbitalMTLSConfig holds mTLS certificate paths.
type OrbitalMTLSConfig struct {
	CertFile   string `yaml:"certFile"   mapstructure:"certFile"`
	KeyFile    string `yaml:"keyFile"    mapstructure:"keyFile"`
	CAFile     string `yaml:"caFile"     mapstructure:"caFile"`
	ServerName string `yaml:"serverName" mapstructure:"serverName"`
}

// OrbitalBasicConfig holds SASL PLAIN credentials.
type OrbitalBasicConfig struct {
	Username string `yaml:"username" mapstructure:"username"`
	Password string `yaml:"password" mapstructure:"password"`
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

// RegistryConfig configures the CMK Registry gRPC connection.
type RegistryConfig struct {
	Enabled     bool   `yaml:"enabled"     mapstructure:"enabled"`
	Address     string `yaml:"address"     mapstructure:"address"`
	Region      string `yaml:"region"      mapstructure:"region"`
	OwnerID     string `yaml:"ownerId"     mapstructure:"ownerId"`
	OwnerType   string `yaml:"ownerType"   mapstructure:"ownerType"`
	Role        string `yaml:"role"        mapstructure:"role"`        // e.g. ROLE_TRIAL (default)
	Insecure    bool   `yaml:"insecure"    mapstructure:"insecure"`
	BearerToken string `yaml:"bearerToken" mapstructure:"bearerToken"`
	CACert      string `yaml:"caCert"      mapstructure:"caCert"`
	ClientCert  string `yaml:"clientCert"  mapstructure:"clientCert"`
	ClientKey   string `yaml:"clientKey"   mapstructure:"clientKey"`
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
	v.SetDefault("orbital.amqp.url", "amqps://kms-rabbitmqcluster.services.svc.cluster.local:5671")
	v.SetDefault("orbital.amqp.auth.type", "mtls")
	v.SetDefault("keyHierarchy.maxDepth", 4)
	v.SetDefault("database.dsn", "")
	v.SetDefault("worker.concurrency", 3)
	v.SetDefault("worker.pollInterval", "500ms")
	v.SetDefault("worker.maxAttempts", 10)
	v.SetDefault("worker.stuckTimeout", "5m")
	v.SetDefault("registry.enabled", false)
	v.SetDefault("registry.address", "localhost:9093")
	v.SetDefault("registry.region", "eu-central")

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
