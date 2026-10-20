//go:build helmtests

package unit_test

import (
	"strings"
	"testing"
)

// resource is the kind and name of a rendered resource.
type resource struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
}

// resources renders the chart and returns its resources as "Kind/name".
func resources(t *testing.T, args ...string) []string {
	t.Helper()

	output := render(t, args...)

	var result []string

	for _, kind := range []string{
		"ConfigMap", "Deployment", "HorizontalPodAutoscaler", "NetworkPolicy",
		"PodDisruptionBudget", "Service", "ServiceAccount",
	} {
		for _, r := range objects[resource](t, output, kind) {
			if r.Metadata.Namespace != namespace {
				t.Errorf("%s/%s is in namespace %q, want %q", r.Kind, r.Metadata.Name, r.Metadata.Namespace, namespace)
			}

			result = append(result, r.Kind+"/"+r.Metadata.Name)
		}
	}

	return result
}

func TestChartResources(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "defaults",
			want: []string{"ConfigMap/" + fullName + "-config", "Deployment/" + fullName, "Service/" + fullName, "ServiceAccount/" + fullName},
		},
		{
			name: "all optional resources",
			args: []string{
				"--set", "hpa.enabled=true", "--set", "networkPolicy.enabled=true",
				"--set", "pod.disruptionBudget.enabled=true",
			},
			want: []string{
				"ConfigMap/" + fullName + "-config", "Deployment/" + fullName, "HorizontalPodAutoscaler/" + fullName,
				"NetworkPolicy/" + fullName, "PodDisruptionBudget/" + fullName, "Service/" + fullName,
				"ServiceAccount/" + fullName,
			},
		},
		{
			name: "full name override without ServiceAccount",
			args: []string{"--set", "fullnameOverride=bridge-eu10", "--set", "serviceAccount.create=false"},
			want: []string{"ConfigMap/bridge-eu10-config", "Deployment/bridge-eu10", "Service/bridge-eu10"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strings.Join(resources(t, tt.args...), " "); got != strings.Join(tt.want, " ") {
				t.Errorf("resources:\n%s\nwant:\n%s", got, strings.Join(tt.want, " "))
			}
		})
	}
}

func TestDisruptionBudget(t *testing.T) {
	type pdb struct {
		Spec map[string]any `yaml:"spec"`
	}

	spec := object[pdb](t, render(t, "--set", "pod.disruptionBudget.enabled=true"), "PodDisruptionBudget").Spec
	if spec["maxUnavailable"] != 1 {
		t.Errorf("PodDisruptionBudget %v, want maxUnavailable: 1", spec)
	}
}

func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "database.secretName of chart 0.1.0",
			args: []string{"--set", "database.secretName=bridge-db"},
			want: "set database.existingSecret",
		},
		{
			name: "orbital.rabbitmq.secretName of chart 0.1.0",
			args: []string{"--set", "orbital.rabbitmq.secretName=bridge-rabbitmq-tls"},
			want: "renamed to rabbitmq.existingSecret",
		},
		{
			name: "service.grpcPort of chart 0.1.0",
			args: []string{"--set", "service.grpcPort=9092"},
			want: "service.ports and ports",
		},
		{
			name: "registry host in image.repository (default of chart 0.1.0)",
			args: []string{"--set", "image.repository=ghcr.io/openkcm/images/cmk-krypton-bridge"},
			want: "contains a registry host",
		},
		{
			name: "autoscaler without target",
			args: []string{
				"--set", "hpa.enabled=true",
				"--set", "hpa.targetCPUUtilizationPercentage=null",
				"--set", "hpa.targetMemoryUtilizationPercentage=null",
			},
			want: "hpa.enabled requires",
		},
		{
			name: "disruption budget with both limits",
			args: []string{
				"--set", "pod.disruptionBudget.enabled=true",
				"--set", "pod.disruptionBudget.minAvailable=1",
				"--set", "pod.disruptionBudget.maxUnavailable=1",
			},
			want: "set either minAvailable or maxUnavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := renderError(t, tt.args...); !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q, want %q", err, tt.want)
			}
		})
	}
}
