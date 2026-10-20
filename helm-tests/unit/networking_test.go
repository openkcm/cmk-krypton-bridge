//go:build helmtests

package unit_test

import (
	"slices"
	"strings"
	"testing"
)

// service is the part of the Service that the tests check.
type service struct {
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Type     string            `yaml:"type"`
		Selector map[string]string `yaml:"selector"`
		Ports    []struct {
			Name       string `yaml:"name"`
			Port       int    `yaml:"port"`
			TargetPort string `yaml:"targetPort"`
		} `yaml:"ports"`
	} `yaml:"spec"`
}

// networkPolicy is the part of the NetworkPolicy that the tests check.
type networkPolicy struct {
	Spec struct {
		PolicyTypes []string `yaml:"policyTypes"`
		Ingress     []struct {
			Ports []struct {
				Port string `yaml:"port"`
			} `yaml:"ports"`
			From []map[string]any `yaml:"from"`
		} `yaml:"ingress"`
	} `yaml:"spec"`
}

// TestPorts checks that the Service ports target container ports, and that the gRPC port is
// the one of config.grpcServer.address.
func TestPorts(t *testing.T) {
	output := render(t)
	s := object[service](t, output, "Service")
	_, c := bridgeContainer(t)

	containerPorts := map[string]int{}
	for _, p := range c.Ports {
		containerPorts[p.Name] = p.ContainerPort
	}

	var ports []string
	for _, p := range s.Spec.Ports {
		ports = append(ports, p.Name)
		if _, ok := containerPorts[p.TargetPort]; !ok {
			t.Errorf("Service port %s targets %q, which is not a container port", p.Name, p.TargetPort)
		}
	}

	if !slices.Equal(ports, []string{"grpc", "http"}) || s.Spec.Type != "ClusterIP" {
		t.Errorf("Service %s with ports %v", s.Spec.Type, ports)
	}

	cfg := loadValues(t)
	if !strings.HasSuffix(cfg.GRPCServer.Address, ":9092") || containerPorts["grpc"] != 9092 || containerPorts["http"] != 8080 {
		t.Errorf("gRPC address %q, container ports %v", cfg.GRPCServer.Address, containerPorts)
	}
}

func TestNetworkPolicy(t *testing.T) {
	if policies := objects[networkPolicy](t, render(t), "NetworkPolicy"); len(policies) != 0 {
		t.Errorf("%d NetworkPolicies by default", len(policies))
	}

	np := object[networkPolicy](t, render(t, "--set", "networkPolicy.enabled=true"), "NetworkPolicy")
	if !slices.Equal(np.Spec.PolicyTypes, []string{"Ingress"}) || len(np.Spec.Ingress) != 0 {
		t.Errorf("NetworkPolicy %+v, want Ingress without rules", np.Spec)
	}

	values := "networkPolicy:\n  enabled: true\n  grpc:\n    from:\n" +
		indent(valuesComment(t, "NetworkPolicy peer syntax, for example:"), "      ") +
		"  http:\n    from:\n      - namespaceSelector: { matchLabels: { kubernetes.io/metadata.name: monitoring } }\n"
	np = object[networkPolicy](t, render(t, "-f", writeValues(t, values)), "NetworkPolicy")

	var rules []string
	for _, rule := range np.Spec.Ingress {
		rules = append(rules, rule.Ports[0].Port+":"+strings.Repeat("peer,", len(rule.From)))
	}

	if !slices.Equal(rules, []string{"grpc:peer,", "http:peer,"}) {
		t.Errorf("ingress rules %v", rules)
	}
}
