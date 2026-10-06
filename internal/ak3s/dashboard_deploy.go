package ak3s

import (
	"bytes"
	"encoding/json"
	"regexp"
	"text/template"

	"github.com/jgador/ak3s/platform"
	"go.yaml.in/yaml/v3"
)

// DashboardImage is the release image, set to an immutable digest by release builds.
var DashboardImage = ""

var dashboardImageRE = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*(?::[0-9]+)?(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)*(?::[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}|@sha256:[a-f0-9]{64})$`)

type dashboardMetadata struct {
	ClusterName, APIEndpoint, AK3SVersion, Datastore, NodeName string
	Platform                                                   bool
	Configuration                                              dashboardConfiguration
}

type dashboardConfiguration struct {
	Path          string `json:"path"`
	OverrideCount int    `json:"overrideCount"`
	EffectiveYAML string `json:"effectiveYaml"`
	OverridesYAML string `json:"overridesYaml"`
}

func dashboardImage(c Config) string {
	if c.DashboardImage != "" {
		return c.DashboardImage
	}
	if DashboardImage != "" {
		return DashboardImage
	}
	return "ghcr.io/jgador/ak3s-dashboard:" + Version
}

func dashboardMetadataFor(c Config) (dashboardMetadata, error) {
	raw, err := yaml.Marshal(c)
	if err != nil {
		return dashboardMetadata{}, err
	}
	effective, err := mapping(raw)
	if err != nil {
		return dashboardMetadata{}, err
	}
	path := c.sourcePath
	if path == "" {
		path = "/etc/ak3s/values.yaml"
	}
	return dashboardMetadata{
		ClusterName: c.ClusterName, APIEndpoint: "https://" + c.APIEndpoint + ":6443",
		AK3SVersion: Version, Datastore: c.Node.Datastore, NodeName: c.Node.Name, Platform: c.Platform,
		Configuration: dashboardConfiguration{Path: path, OverrideCount: c.overrideCount,
			EffectiveYAML: dashboardConfigYAML(effective), OverridesYAML: c.displayOverrides},
	}, nil
}

// DashboardManifests renders the private dashboard and its sanitized configuration.
func DashboardManifests(c Config) ([]byte, error) {
	metadata, err := dashboardMetadataFor(c)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	raw, err := platform.Files.ReadFile("manifests/dashboard.yaml")
	if err != nil {
		return nil, err
	}
	t, err := template.New("dashboard").Funcs(template.FuncMap{"quote": func(s string) string { b, _ := json.Marshal(s); return string(b) }}).Parse(string(raw))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	err = t.Execute(&out, map[string]string{"Image": dashboardImage(c), "Metadata": string(data), "Checksum": fingerprint(data)})
	return out.Bytes(), err
}
