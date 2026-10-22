package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/openkcm/common-sdk/pkg/commoncfg"
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
	Enabled bool              `yaml:"enabled" mapstructure:"enabled" default:"false"`
	AMQP    OrbitalAMQPConfig `yaml:"amqp"    mapstructure:"amqp"`
}

// OrbitalAMQPConfig holds AMQP broker connection details for the orbital Operator.
type OrbitalAMQPConfig struct {
	URL    string                `yaml:"url"      mapstructure:"url" default:"amqps://kms-rabbitmqcluster.services.svc.cluster.local:5671"` // e.g. amqps://kms-rabbitmqcluster.services.svc.cluster.local:5671
	Target string                `yaml:"target"   mapstructure:"target"`                                                                    // exchange to send responses to (CMK listens here)
	Source string                `yaml:"source"   mapstructure:"source"`                                                                    // queue Bridge listens on for incoming tasks
	Auth   OrbitalAMQPAuthConfig `yaml:"auth"     mapstructure:"auth"`
}

// OrbitalAMQPAuthConfig selects the auth method for AMQP.
type OrbitalAMQPAuthConfig struct {
	Type  string             `yaml:"type"  mapstructure:"type" default:"mtls"` // "mtls", "basic", or "anonymous"
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
	MaxDepth int `yaml:"maxDepth" mapstructure:"maxDepth" default:"4"`
}

// DatabaseConfig holds the PostgreSQL connection string.
type DatabaseConfig struct {
	DSN string `yaml:"dsn" mapstructure:"dsn" default:""`
}

// WorkerConfig controls the background polling worker pool.
type WorkerConfig struct {
	Concurrency  int           `yaml:"concurrency"  mapstructure:"concurrency" default:"3"`
	PollInterval time.Duration `yaml:"pollInterval" mapstructure:"pollInterval" default:"500ms"`
	MaxAttempts  int           `yaml:"maxAttempts"  mapstructure:"maxAttempts" default:"10"`
	StuckTimeout time.Duration `yaml:"stuckTimeout" mapstructure:"stuckTimeout" default:"5m"`
}

// RegistryConfig configures the CMK Registry gRPC connection.
type RegistryConfig struct {
	Enabled     bool   `yaml:"enabled"     mapstructure:"enabled" default:"false"`
	Address     string `yaml:"address"     mapstructure:"address" default:"localhost:9093"`
	Region      string `yaml:"region"      mapstructure:"region" default:"eu-central"`
	OwnerID     string `yaml:"ownerId"     mapstructure:"ownerId"`
	OwnerType   string `yaml:"ownerType"   mapstructure:"ownerType"`
	SystemType  string `yaml:"systemType"  mapstructure:"systemType"` // required by registry when registering systems
	Role        string `yaml:"role"        mapstructure:"role"`       // e.g. ROLE_TRIAL (default)
	Insecure    bool   `yaml:"insecure"    mapstructure:"insecure"`
	BearerToken string `yaml:"bearerToken" mapstructure:"bearerToken"`
	CACert      string `yaml:"caCert"      mapstructure:"caCert"`
	ClientCert  string `yaml:"clientCert"  mapstructure:"clientCert"`
	ClientKey   string `yaml:"clientKey"   mapstructure:"clientKey"`
}

// Load reads configuration from file and environment variables.
// Env vars override file values. Prefix: KRYPTON_PROXY_
func Load(cfgFile string) (*Config, error) {
	if cfgFile == "" {
		cfgFile = commoncfg.DefaultFileName + ".yaml"
	}

	cfg := &Config{}
	configName := filepath.Base(cfgFile)
	configFormat := commoncfg.YAMLFileFormat
	if extension := filepath.Ext(configName); extension != "" {
		configName = strings.TrimSuffix(configName, extension)
		configFormat = commoncfg.FileFormat(strings.TrimPrefix(extension, "."))
	}

	// Defaults for commoncfg-owned fields belong here; mockMode also stays here so explicit false values are preserved.
	defaultValues := map[string]any{
		"application.name":              "cmk-krypton-bridge",
		"application.buildInfo.version": "0.1.0",
		"grpcServer.enabled":            true,
		"kryptonClient.address":         "localhost:9090",
		"kryptonClient.mockMode":        true,
	}
	loader := commoncfg.NewLoader(
		cfg,
		commoncfg.WithDefaults(defaultValues),
		commoncfg.WithFile(configName, configFormat),
		commoncfg.WithPaths(filepath.Dir(cfgFile)),
		commoncfg.WithEnvOverride("KRYPTON_PROXY"),
		commoncfg.DisableViperErrorUnused(),
	)
	if err := loader.LoadConfig(); err != nil {
		return nil, fmt.Errorf("loading config file %q: %w", cfgFile, err)
	}

	return cfg, nil
}
