//go:build helmtests

package unit_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-viper/mapstructure/v2"
	"go.yaml.in/yaml/v3"

	"github.com/openkcm/cmk-krypton-bridge/pkg/config"
)

// loadConfig writes config.yaml to a directory and loads it the way the bridge does at
// startup (cmd/cmk-krypton-bridge/main.go): config.Load, which reads the file and the KRYPTON_PROXY_*
// environment variables. Set the variables with t.Setenv before.
func loadConfig(t *testing.T, configYAML string) *config.Config {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")

	err := os.WriteFile(path, []byte(configYAML), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("the bridge does not load the configuration: %v\n%s", err, configYAML)
	}

	return cfg
}

// unreadKeys returns the keys of configYAML that the bridge does not read. The bridge ignores
// unknown keys, so a misspelled key would go unnoticed: this decodes configYAML into the
// configuration type as the bridge does (mapstructure, key names case-insensitive) and
// returns the keys that no field takes.
func unreadKeys(t *testing.T, configYAML string) []string {
	t.Helper()

	var raw map[string]any

	err := yaml.Unmarshal([]byte(configYAML), &raw)
	if err != nil {
		t.Fatal(err)
	}

	var metadata mapstructure.Metadata

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook:       mapstructure.StringToTimeDurationHookFunc(),
		Metadata:         &metadata,
		Result:           &config.Config{},
		WeaklyTypedInput: true,
		MatchName:        strings.EqualFold,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = decoder.Decode(raw)
	if err != nil {
		t.Fatalf("decoding the configuration: %v", err)
	}

	slices.Sort(metadata.Unused)

	return metadata.Unused
}

// loadValues renders the chart with the arguments and loads the configuration the way the
// bridge does at startup. It fails the test if the bridge does not read all of it.
func loadValues(t *testing.T, args ...string) *config.Config {
	t.Helper()

	configYAML := renderConfigYAML(t, args...)
	if unread := unreadKeys(t, configYAML); len(unread) > 0 {
		t.Fatalf("keys that the bridge does not read: %v\n%s", unread, configYAML)
	}

	return loadConfig(t, configYAML)
}

func TestConfigDefaults(t *testing.T) {
	cfg := loadValues(t)

	if cfg.KryptonClient.MockMode || cfg.KryptonClient.Address != "krypton-root.services.svc.cluster.local:8080" ||
		cfg.KryptonClient.SecretRef == nil || cfg.KryptonClient.SecretRef.Type != "insecure" {
		t.Errorf("kryptonClient %+v, want the krypton-root Service without mock mode", cfg.KryptonClient)
	}

	if !cfg.Orbital.Enabled || cfg.Orbital.AMQP.Auth.Type != "mtls" ||
		!strings.HasPrefix(cfg.Orbital.AMQP.Auth.MTLS.CertFile, "/etc/credentials/rabbitmq/") {
		t.Errorf("orbital %+v, want AMQP with mutual TLS from /etc/credentials/rabbitmq", cfg.Orbital)
	}

	if !cfg.Registry.Enabled || !cfg.Registry.Insecure || cfg.Registry.Region == "" {
		t.Errorf("registry %+v", cfg.Registry)
	}

	if cfg.Database.DSN != "" || cfg.GRPCServer.Address != ":9092" || cfg.Logger.Level != "info" {
		t.Errorf("database %q, gRPC address %q, log level %q", cfg.Database.DSN, cfg.GRPCServer.Address, cfg.Logger.Level)
	}
}

func TestConfigRuntimeDefaults(t *testing.T) {
	cfg := loadConfig(t, "{}")

	if cfg.Application.Name != "cmk-krypton-bridge" || cfg.Application.BuildInfo.Version != "0.1.0" ||
		cfg.Logger.Level != "info" || string(cfg.Logger.Format) != "json" ||
		!cfg.GRPCServer.Enabled || cfg.GRPCServer.Address != ":9092" {
		t.Errorf("application %+v, logger %+v, gRPC server %+v", cfg.Application, cfg.Logger, cfg.GRPCServer)
	}
	if !cfg.KryptonClient.MockMode || cfg.KryptonClient.Address != "localhost:9090" ||
		cfg.Orbital.Enabled || cfg.Orbital.AMQP.URL != "amqps://kms-rabbitmqcluster.services.svc.cluster.local:5671" ||
		cfg.Orbital.AMQP.Auth.Type != "mtls" || cfg.KeyHierarchy.MaxDepth != 4 {
		t.Errorf("Krypton client %+v, orbital %+v, key hierarchy %+v", cfg.KryptonClient, cfg.Orbital, cfg.KeyHierarchy)
	}
	if cfg.Database.DSN != "" || cfg.Worker.Concurrency != 3 || cfg.Worker.PollInterval.String() != "500ms" ||
		cfg.Worker.MaxAttempts != 10 || cfg.Worker.StuckTimeout.String() != "5m0s" ||
		cfg.Registry.Enabled || cfg.Registry.Address != "localhost:9093" || cfg.Registry.Region != "eu-central" {
		t.Errorf("database %q, worker %+v, registry %+v", cfg.Database.DSN, cfg.Worker, cfg.Registry)
	}
}

// TestConfigPassThrough checks that everything under config except isImmutable reaches
// config.yaml, including the keys that chart 0.1.0 dropped, and that the unread-keys check of
// these tests finds keys that the bridge ignores.
func TestConfigPassThrough(t *testing.T) {
	cm := object[configMap](t, render(t,
		"--set", "config.isImmutable=true",
		"--set", "config.registry.systemType=KRYPTON",
		"--set", "config.orbital.amqp.auth.mtls.serverName=rabbitmq.example.org",
		"--set", "config.futureSetting=on",
	), "ConfigMap")

	if !cm.Immutable {
		t.Error("config.isImmutable does not make the ConfigMap immutable")
	}

	configYAML := cm.Data["config.yaml"]
	if strings.Contains(configYAML, "isImmutable") {
		t.Errorf("config.yaml contains the chart setting isImmutable:\n%s", configYAML)
	}

	if unread := unreadKeys(t, configYAML); !slices.Equal(unread, []string{"futureSetting"}) {
		t.Errorf("unread keys: %v, want [futureSetting]", unread)
	}

	cfg := loadConfig(t, configYAML)
	if cfg.Registry.SystemType != "KRYPTON" || cfg.Orbital.AMQP.Auth.MTLS.ServerName != "rabbitmq.example.org" {
		t.Errorf("systemType %q, serverName %q do not reach the bridge", cfg.Registry.SystemType,
			cfg.Orbital.AMQP.Auth.MTLS.ServerName)
	}
}

// TestConfigEnvironment checks the environment variables that values.yaml documents: the
// connection string from database.existingSecret, and secrets for keys that are in config with
// an empty value.
func TestConfigEnvironment(t *testing.T) {
	_, c := bridgeContainer(t, "--set", "database.existingSecret=bridge-db")
	if got := env(c)["KRYPTON_PROXY_DATABASE_DSN"]; got != "secret bridge-db/DATABASE_DSN" {
		t.Fatalf("KRYPTON_PROXY_DATABASE_DSN %q, want the key DATABASE_DSN of bridge-db", got)
	}

	values := writeValues(t, `config:
  orbital:
    amqp:
      auth:
        type: basic
        basic: { username: bridge, password: "" }
  registry:
    bearerToken: ""
`)
	configYAML := renderConfigYAML(t, "-f", values)

	t.Setenv("KRYPTON_PROXY_DATABASE_DSN", "postgres://bridge@db/bridge")
	t.Setenv("KRYPTON_PROXY_ORBITAL_AMQP_AUTH_BASIC_PASSWORD", "amqp-secret")
	t.Setenv("KRYPTON_PROXY_REGISTRY_BEARERTOKEN", "registry-token")

	cfg := loadConfig(t, configYAML)
	if cfg.Database.DSN != "postgres://bridge@db/bridge" || cfg.Orbital.AMQP.Auth.Basic.Password != "amqp-secret" ||
		cfg.Registry.BearerToken != "registry-token" {
		t.Errorf("database %q, AMQP password %q, registry token %q: the variables do not take effect",
			cfg.Database.DSN, cfg.Orbital.AMQP.Auth.Basic.Password, cfg.Registry.BearerToken)
	}

	// Without the key in config, the variable has no effect.
	cfg = loadConfig(t, renderConfigYAML(t))
	if cfg.Registry.BearerToken != "" {
		t.Errorf("registry token %q without the key in config", cfg.Registry.BearerToken)
	}
}

// TestReadmeValues installs the chart with the values file of the README: the bridge loads
// the configuration, and the release notes have no warning.
func TestReadmeValues(t *testing.T) {
	readme, err := os.ReadFile(chartPath + "/README.md")
	if err != nil {
		t.Fatal(err)
	}

	_, rest, found := strings.Cut(string(readme), "Create a values file")
	if found {
		_, rest, found = strings.Cut(rest, "```yaml\n")
	}

	example, _, closed := strings.Cut(rest, "```")
	if !found || !closed {
		t.Fatal("README.md has no complete values file example")
	}

	values := writeValues(t, example)

	cfg := loadValues(t, "-f", values)
	if cfg.Registry.Region != "eu10" || !strings.Contains(cfg.Orbital.AMQP.Source, ".eu10.") {
		t.Errorf("region %q, source %q", cfg.Registry.Region, cfg.Orbital.AMQP.Source)
	}

	if notes := releaseNotes(t, "-f", values); strings.Contains(notes, "WARNINGS") {
		t.Errorf("warnings with the README values:\n%s", notes)
	}
}

// TestConfigKryptonMTLS loads the mutual TLS example of config.kryptonClient.
func TestConfigKryptonMTLS(t *testing.T) {
	values := "config:\n  kryptonClient:\n" +
		indent(valuesComment(t, "(client certificate, for a root node with `config.auth`):"), "    ")
	cfg := loadValues(t, "-f", writeValues(t, values))

	ref := cfg.KryptonClient.SecretRef
	if ref == nil || ref.Type != "mtls" || ref.MTLS.Cert.File.Path != "/etc/credentials/krypton/tls.crt" ||
		ref.MTLS.ServerCA == nil || ref.MTLS.ServerCA.File.Path != "/etc/credentials/krypton/ca.crt" {
		t.Errorf("secretRef %+v", ref)
	}
}
