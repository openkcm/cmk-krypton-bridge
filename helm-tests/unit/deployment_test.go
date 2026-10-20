//go:build helmtests

package unit_test

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// The resource names of the default release: `<release name>-cmk-krypton-bridge`, as in chart
// 0.1.0.
const fullName = releaseName + "-cmk-krypton-bridge"

// deployment is the part of the Deployment that the tests check.
type deployment struct {
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Replicas *int `yaml:"replicas"`
		Selector struct {
			MatchLabels map[string]string `yaml:"matchLabels"`
		} `yaml:"selector"`
		Template struct {
			Metadata struct {
				Annotations map[string]string `yaml:"annotations"`
				Labels      map[string]string `yaml:"labels"`
			} `yaml:"metadata"`
			Spec struct {
				AutomountServiceAccountToken *bool `yaml:"automountServiceAccountToken"`
				EnableServiceLinks           *bool `yaml:"enableServiceLinks"`
				SecurityContext              struct {
					RunAsNonRoot   bool  `yaml:"runAsNonRoot"`
					RunAsUser      int64 `yaml:"runAsUser"`
					RunAsGroup     int64 `yaml:"runAsGroup"`
					FSGroup        int64 `yaml:"fsGroup"`
					SeccompProfile struct {
						Type string `yaml:"type"`
					} `yaml:"seccompProfile"`
				} `yaml:"securityContext"`
				Containers []container `yaml:"containers"`
				Volumes    []volume    `yaml:"volumes"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

type volume struct {
	Name      string `yaml:"name"`
	ConfigMap *struct {
		Name string `yaml:"name"`
	} `yaml:"configMap"`
	Secret *struct {
		SecretName string `yaml:"secretName"`
	} `yaml:"secret"`
}

type container struct {
	Name            string   `yaml:"name"`
	Image           string   `yaml:"image"`
	Command         []string `yaml:"command"`
	Args            []string `yaml:"args"`
	SecurityContext struct {
		AllowPrivilegeEscalation *bool `yaml:"allowPrivilegeEscalation"`
		ReadOnlyRootFilesystem   bool  `yaml:"readOnlyRootFilesystem"`
		Capabilities             struct {
			Drop []string `yaml:"drop"`
		} `yaml:"capabilities"`
	} `yaml:"securityContext"`
	Ports []struct {
		Name          string `yaml:"name"`
		ContainerPort int    `yaml:"containerPort"`
	} `yaml:"ports"`
	Env []struct {
		Name      string `yaml:"name"`
		Value     string `yaml:"value"`
		ValueFrom *struct {
			SecretKeyRef *struct {
				Name string `yaml:"name"`
				Key  string `yaml:"key"`
			} `yaml:"secretKeyRef"`
		} `yaml:"valueFrom"`
	} `yaml:"env"`
	StartupProbe   *probe `yaml:"startupProbe"`
	LivenessProbe  *probe `yaml:"livenessProbe"`
	ReadinessProbe *probe `yaml:"readinessProbe"`
	Resources      struct {
		Requests map[string]string `yaml:"requests"`
		Limits   map[string]string `yaml:"limits"`
	} `yaml:"resources"`
	VolumeMounts []struct {
		Name      string `yaml:"name"`
		MountPath string `yaml:"mountPath"`
		SubPath   string `yaml:"subPath"`
		ReadOnly  bool   `yaml:"readOnly"`
	} `yaml:"volumeMounts"`
}

type probe struct {
	HTTPGet *struct {
		Path string `yaml:"path"`
		Port string `yaml:"port"`
	} `yaml:"httpGet"`
	PeriodSeconds    int `yaml:"periodSeconds"`
	FailureThreshold int `yaml:"failureThreshold"`
}

// bridgeContainer renders the chart and returns the Deployment and its bridge container.
func bridgeContainer(t *testing.T, args ...string) (deployment, container) {
	t.Helper()

	d := object[deployment](t, render(t, args...), "Deployment")
	for _, c := range d.Spec.Template.Spec.Containers {
		if c.Name == "cmk-krypton-bridge" {
			return d, c
		}
	}

	t.Fatalf("the Deployment has no container cmk-krypton-bridge: %+v", d.Spec.Template.Spec.Containers)

	return d, container{}
}

// env returns the environment variables of the container by name.
func env(c container) map[string]string {
	result := map[string]string{}

	for _, e := range c.Env {
		if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
			result[e.Name] = "secret " + e.ValueFrom.SecretKeyRef.Name + "/" + e.ValueFrom.SecretKeyRef.Key
		} else {
			result[e.Name] = e.Value
		}
	}

	return result
}

// mounts returns the volume mounts of the container as name → mount path.
func mounts(c container) map[string]string {
	result := map[string]string{}
	for _, m := range c.VolumeMounts {
		result[m.Name] = m.MountPath
	}

	return result
}

func TestImage(t *testing.T) {
	// The release pipeline (openkcm/build) publishes the image named after the repository as
	// ghcr.io/openkcm/images/<repository>; the Publish Demo workflow keeps its own path.
	const (
		repository = "ghcr.io/openkcm/images/cmk-krypton-bridge"
		demo       = "ghcr.io/openkcm/cmk-krypton-bridge/images/cmk-krypton-bridge"
	)

	tests := []struct {
		name  string
		args  []string
		image string
	}{
		{name: "default: the chart's appVersion", image: repository + ":" + appVersion(t)},
		{name: "tag", args: []string{"--set", "image.tag=v0.0.2"}, image: repository + ":v0.0.2"},
		{
			name: "demo image",
			args: []string{
				"--set", "image.repository=cmk-krypton-bridge/images/cmk-krypton-bridge",
				"--set", "image.tag=demo-d878ad0",
			},
			image: demo + ":demo-d878ad0",
		},
		{
			name:  "the digest takes precedence over the tag",
			args:  []string{"--set", "image.tag=v9.9.9", "--set", "image.digest=sha256:0123456789abcdef"},
			image: repository + "@sha256:0123456789abcdef",
		},
		{
			name: "full name in the repository",
			args: []string{
				"--set", "image.registry=",
				"--set", "image.repository=" + demo, "--set", "image.tag=demo-e784835",
			},
			image: demo + ":demo-e784835",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, c := bridgeContainer(t, tt.args...)
			if c.Image != tt.image {
				t.Errorf("image %q, want %q", c.Image, tt.image)
			}
		})
	}
}

func TestCommandAndArgs(t *testing.T) {
	_, c := bridgeContainer(t)
	if len(c.Command) != 0 || !slices.Equal(c.Args, []string{"--config=/etc/cmk-krypton-bridge/config.yaml"}) {
		t.Errorf("command %q, args %q; want the image's command with --config", c.Command, c.Args)
	}

	_, c = bridgeContainer(t, "--set", "image.command=/cmk2kryptonctl", "--set", "image.args[0]=--extra")
	if !slices.Equal(c.Command, []string{"/cmk2kryptonctl"}) || !slices.Contains(c.Args, "--extra") {
		t.Errorf("command %q, args %q", c.Command, c.Args)
	}
}

func TestSecurityDefaults(t *testing.T) {
	d, c := bridgeContainer(t)
	pod := d.Spec.Template.Spec

	if !pod.SecurityContext.RunAsNonRoot || pod.SecurityContext.RunAsUser != 65532 ||
		pod.SecurityContext.RunAsGroup != 65532 || pod.SecurityContext.FSGroup != 65532 ||
		pod.SecurityContext.SeccompProfile.Type != "RuntimeDefault" {
		t.Errorf("pod security context: %+v", pod.SecurityContext)
	}

	if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Error("the ServiceAccount token is mounted; the bridge does not call the Kubernetes API")
	}

	if pod.EnableServiceLinks == nil || *pod.EnableServiceLinks {
		t.Error("Service environment variables are enabled")
	}

	sc := c.SecurityContext
	if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation || !sc.ReadOnlyRootFilesystem ||
		!slices.Equal(sc.Capabilities.Drop, []string{"ALL"}) {
		t.Errorf("container security context: %+v", sc)
	}

	if c.Resources.Requests["cpu"] == "" || c.Resources.Limits["memory"] == "" ||
		c.Resources.Limits["ephemeral-storage"] == "" {
		t.Errorf("resources: %+v", c.Resources)
	}

	if len(c.Env) != 0 {
		t.Errorf("environment by default: %v", env(c))
	}
}

func TestVolumes(t *testing.T) {
	d, c := bridgeContainer(t)

	want := map[string]string{"config": "/etc/cmk-krypton-bridge/config.yaml", "tmp": "/tmp"}
	if got := mounts(c); !maps.Equal(got, want) {
		t.Errorf("volume mounts %v, want %v", got, want)
	}

	for _, v := range d.Spec.Template.Spec.Volumes {
		if v.Name == "config" && (v.ConfigMap == nil || v.ConfigMap.Name != fullName+"-config") {
			t.Errorf("config volume %+v", v)
		}
	}

	d, c = bridgeContainer(t, "--set", "rabbitmq.existingSecret=bridge-rabbitmq-tls")
	if mounts(c)["rabbitmq-credentials"] != "/etc/credentials/rabbitmq" {
		t.Errorf("volume mounts %v, want rabbitmq-credentials at /etc/credentials/rabbitmq", mounts(c))
	}

	if !slices.ContainsFunc(d.Spec.Template.Spec.Volumes, func(v volume) bool {
		return v.Secret != nil && v.Secret.SecretName == "bridge-rabbitmq-tls"
	}) {
		t.Error("no volume of the Secret bridge-rabbitmq-tls")
	}
}

// TestMountedFiles checks that the AMQP certificate files of the default configuration are on
// the volume of rabbitmq.existingSecret, and the files of the Krypton mTLS example on the volume
// of a Secret mounted as values.yaml shows.
func TestMountedFiles(t *testing.T) {
	cfg := loadValues(t)
	_, c := bridgeContainer(t, "--set", "rabbitmq.existingSecret=bridge-rabbitmq-tls")

	for _, file := range []string{
		cfg.Orbital.AMQP.Auth.MTLS.CertFile, cfg.Orbital.AMQP.Auth.MTLS.KeyFile, cfg.Orbital.AMQP.Auth.MTLS.CAFile,
	} {
		if !strings.HasPrefix(file, mounts(c)["rabbitmq-credentials"]+"/") {
			t.Errorf("%s is not on the volume of rabbitmq.existingSecret", file)
		}
	}

	values := "extraVolumes:\n" + indent(valuesComment(t, "Example of a volume for mutual TLS with the Registry:"), "  ") +
		"extraVolumeMounts:\n" + indent(valuesComment(t, "the mount for the volume above:"), "  ")
	_, c = bridgeContainer(t, "-f", writeValues(t, values))

	if mounts(c)["registry-tls"] != "/etc/credentials/registry" {
		t.Errorf("volume mounts of the example: %v", mounts(c))
	}
}

func TestProbes(t *testing.T) {
	_, c := bridgeContainer(t)

	for name, p := range map[string]*probe{"startup": c.StartupProbe, "liveness": c.LivenessProbe} {
		if p == nil || p.HTTPGet == nil || p.HTTPGet.Path != "/healthz" || p.HTTPGet.Port != "http" {
			t.Errorf("%s probe %+v, want /healthz on port http", name, p)
		}
	}

	if p := c.ReadinessProbe; p == nil || p.HTTPGet == nil || p.HTTPGet.Path != "/readyz" {
		t.Errorf("readiness probe %+v, want /readyz", p)
	}

	if c.StartupProbe.PeriodSeconds*c.StartupProbe.FailureThreshold < 120 {
		t.Errorf("the startup probe allows less than 120 seconds: %+v", c.StartupProbe)
	}
}

func TestChecksumConfig(t *testing.T) {
	checksum := func(args ...string) string {
		d, _ := bridgeContainer(t, args...)
		return d.Spec.Template.Metadata.Annotations["checksum/config"]
	}

	defaults := checksum()
	if defaults == "" {
		t.Fatal("no checksum/config annotation")
	}

	if defaults == checksum("--set", "config.worker.concurrency=5") {
		t.Error("the checksum does not change with the configuration")
	}

	if defaults != checksum("--set", "pod.annotations.team=keys") {
		t.Error("the checksum changes without a configuration change")
	}
}

func TestReplicas(t *testing.T) {
	d, _ := bridgeContainer(t)
	if d.Spec.Replicas == nil || *d.Spec.Replicas != 1 {
		t.Errorf("replicas %v, want 1", d.Spec.Replicas)
	}

	d, _ = bridgeContainer(t, "--set", "hpa.enabled=true")
	if d.Spec.Replicas != nil {
		t.Errorf("replicas %d with the autoscaler", *d.Spec.Replicas)
	}
}

// TestNamesAndSelectors checks the resource names and selector labels of chart 0.1.0: a
// Deployment's selector cannot change on upgrade.
func TestNamesAndSelectors(t *testing.T) {
	output := render(t)
	want := map[string]string{"app.kubernetes.io/name": "cmk-krypton-bridge", "app.kubernetes.io/instance": releaseName}

	d := object[deployment](t, output, "Deployment")
	if d.Metadata.Name != fullName || !maps.Equal(d.Spec.Selector.MatchLabels, want) {
		t.Errorf("Deployment %q with selector %v, want %s with %v", d.Metadata.Name, d.Spec.Selector.MatchLabels,
			fullName, want)
	}

	s := object[service](t, output, "Service")
	if s.Metadata.Name != fullName || !maps.Equal(s.Spec.Selector, want) {
		t.Errorf("Service %q with selector %v", s.Metadata.Name, s.Spec.Selector)
	}

	if v := d.Spec.Template.Metadata.Labels["app.kubernetes.io/version"]; v != appVersion(t) {
		t.Errorf("version label %q, want %q", v, appVersion(t))
	}
}
