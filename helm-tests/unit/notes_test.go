//go:build helmtests

package unit_test

import (
	"strings"
	"testing"
)

// releaseNotes renders the release notes (templates/NOTES.txt) with the arguments.
func releaseNotes(t *testing.T, args ...string) string {
	t.Helper()

	out, err := runHelm(t, append([]string{"install", releaseName, chartPath, "--dry-run=client"}, args...)...)
	if err != nil {
		t.Fatalf("helm install --dry-run failed: %v", err)
	}

	_, notes, found := strings.Cut(out, "NOTES:\n")
	if !found {
		t.Fatalf("no release notes in:\n%s", out)
	}

	return notes
}

// Values with which the bridge starts and registers its systems.
var startingValues = []string{
	"--set", "database.existingSecret=bridge-db",
	"--set", "rabbitmq.existingSecret=bridge-rabbitmq-tls",
	"--set", "config.registry.ownerId=krypton-bridge",
	"--set", "config.registry.systemType=krypton-bridge",
	"--set", "networkPolicy.enabled=true",
}

func TestReleaseNotes(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		warnings []string
	}{
		{
			name: "defaults",
			warnings: []string{
				"No database connection string",
				"The AMQP client certificate /etc/credentials/rabbitmq/tls.crt is not on a mounted volume",
				"config.registry.ownerId is empty",
				"config.registry.systemType is empty",
				"networkPolicy.enabled is false",
			},
		},
		{name: "starting values", args: startingValues},
		{
			name: "secrets from extraEnvs and extraVolumeMounts",
			args: append([]string{
				"--set", "database.existingSecret=",
				"--set", "rabbitmq.existingSecret=",
				"--set", "extraEnvs[0].name=KRYPTON_PROXY_DATABASE_DSN", "--set", "extraEnvs[0].value=postgres://db",
				"--set", "extraVolumeMounts[0].name=amqp", "--set", "extraVolumeMounts[0].mountPath=/etc/credentials/rabbitmq/",
			}, startingValues...),
		},
		{
			name:     "mock mode",
			args:     append([]string{"--set", "config.kryptonClient.mockMode=true", "--set", "database.existingSecret="}, startingValues[2:]...),
			warnings: []string{"config.kryptonClient.mockMode is true"},
		},
		{
			name:     "connection string in the ConfigMap",
			args:     append([]string{"--set", "config.database.dsn=postgres://db"}, startingValues...),
			warnings: []string{"config.database.dsn is set"},
		},
		{
			name: "AMQP without client certificate",
			args: append([]string{"--set", "config.orbital.amqp.auth.type=anonymous", "--set", "rabbitmq.existingSecret="},
				startingValues...),
		},
		{
			name:     "registry disabled",
			args:     []string{"--set", "config.registry.enabled=false", "--set", "database.existingSecret=db", "--set", "rabbitmq.existingSecret=amqp", "--set", "networkPolicy.enabled=true"},
			warnings: nil,
		},
		{
			name:     "debug log",
			args:     append([]string{"--set", "config.logger.level=debug"}, startingValues...),
			warnings: []string{"config.logger.level is debug"},
		},
		{
			name:     "service outside the cluster",
			args:     append([]string{"--set", "service.type=LoadBalancer"}, startingValues...),
			warnings: []string{"service.type is LoadBalancer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notes := releaseNotes(t, tt.args...)

			for _, want := range []string{
				fullName + ".services.svc:9092",
				"port-forward service/" + fullName + " 8080:http",
			} {
				if !strings.Contains(notes, want) {
					t.Errorf("the notes lack %q:\n%s", want, notes)
				}
			}

			_, warnings, _ := strings.Cut(notes, "WARNINGS:\n")
			warnings, _, _ = strings.Cut(warnings, "\n\n")

			lines := strings.Count(warnings, "\n  - ")
			if warnings != "" {
				lines++
			}

			if lines != len(tt.warnings) {
				t.Errorf("%d warnings, want %d:\n%s", lines, len(tt.warnings), warnings)
			}

			for _, want := range tt.warnings {
				if !strings.Contains(warnings, want) {
					t.Errorf("no warning %q:\n%s", want, warnings)
				}
			}
		})
	}
}
