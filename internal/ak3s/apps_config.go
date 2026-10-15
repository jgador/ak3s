package ak3s

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"github.com/jgador/ak3s/applications"
	"go.yaml.in/yaml/v3"
)

// ApplicationsConfig describes optional workloads, independently of node and platform settings.
type ApplicationsConfig struct {
	Apps map[string]ApplicationSpec `yaml:"apps"`
}

// ApplicationSpec selects a recipe, namespace, and application settings.
type ApplicationSpec struct {
	Recipe    string              `yaml:"recipe"`
	Namespace string              `yaml:"namespace"`
	Settings  ApplicationSettings `yaml:"settings"`
}

// ApplicationSettings contains the supported PostgreSQL settings; credentials remain in a Secret.
type ApplicationSettings struct {
	Storage        string `yaml:"storage"`
	StorageClass   string `yaml:"storage_class"`
	ExistingSecret string `yaml:"existing_secret"`
	Database       string `yaml:"database"`
	Username       string `yaml:"username"`
}

type applicationRecipe struct {
	Version         string `yaml:"version"`
	PostgresVersion string `yaml:"postgres_version"`
	Image           string `yaml:"image"`
}

func loadApplicationCatalog() (map[string]applicationRecipe, error) {
	data, err := applications.Files.ReadFile("catalog.yaml")
	if err != nil {
		return nil, err
	}
	var catalog map[string]applicationRecipe
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&catalog); err != nil {
		return nil, err
	}
	for name, recipe := range catalog {
		if name != "postgres" || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(recipe.Version) ||
			!regexp.MustCompile(`^docker\.io/library/postgres:[0-9]+\.[0-9]+-bookworm@sha256:[0-9a-f]{64}$`).MatchString(recipe.Image) ||
			!strings.Contains(recipe.Image, ":"+recipe.PostgresVersion+"-bookworm@") {
			return nil, errors.New("invalid pinned application recipe")
		}
	}
	if len(catalog) != 1 {
		return nil, errors.New("PostgreSQL recipe is missing")
	}
	return catalog, nil
}

// LoadApplicationsConfig validates strict YAML and supplies defaults for each application.
func LoadApplicationsConfig(data []byte) (ApplicationsConfig, error) {
	var c ApplicationsConfig
	raw, err := mapping(data)
	if err != nil {
		return c, err
	}
	encoded, err := yaml.Marshal(raw)
	if err != nil {
		return c, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(encoded))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return c, err
	}
	for name, spec := range c.Apps {
		if spec.Namespace == "" {
			spec.Namespace = "applications"
		}
		if spec.Settings.Storage == "" {
			spec.Settings.Storage = "10Gi"
		}
		if spec.Settings.StorageClass == "" {
			spec.Settings.StorageClass = "local-path"
		}
		if spec.Settings.Database == "" {
			spec.Settings.Database = "app"
		}
		if spec.Settings.Username == "" {
			spec.Settings.Username = "postgres"
		}
		c.Apps[name] = spec
	}
	return c, c.Validate()
}

// Validate checks recipe names, resource names, persistent storage, and credential references.
func (c ApplicationsConfig) Validate() error {
	catalog, err := loadApplicationCatalog()
	if err != nil {
		return err
	}
	reserved := map[string]bool{"ak3s": true, "observability": true, "cert-manager": true, "headlamp": true, "ingress-nginx": true}
	for _, name := range c.names() {
		spec := c.Apps[name]
		if len(name) > 40 || !labelRE.MatchString(name) {
			return errors.New("application names must be DNS labels of at most 40 characters")
		}
		if _, ok := catalog[spec.Recipe]; !ok {
			return fmt.Errorf("application %s: only the postgres recipe is supported", name)
		}
		if !labelRE.MatchString(spec.Namespace) || strings.HasPrefix(spec.Namespace, "kube-") || reserved[spec.Namespace] {
			return fmt.Errorf("application %s: use an application namespace outside the platform namespaces", name)
		}
		if !regexp.MustCompile(`^[1-9][0-9]*(Mi|Gi|Ti)$`).MatchString(spec.Settings.Storage) {
			return fmt.Errorf("application %s: storage must be a positive quantity such as 10Gi", name)
		}
		if _, err := storageBytes(spec.Settings.Storage); err != nil {
			return fmt.Errorf("application %s: storage quantity is too large", name)
		}
		if !hostname(spec.Settings.StorageClass) || !hostname(spec.Settings.ExistingSecret) {
			return fmt.Errorf("application %s: storage_class and existing_secret must be Kubernetes resource names", name)
		}
		for _, value := range []string{spec.Settings.Database, spec.Settings.Username} {
			if !regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`).MatchString(value) {
				return fmt.Errorf("application %s: database and username must be lowercase PostgreSQL identifiers", name)
			}
		}
		if spec.Settings.Database == "template0" || spec.Settings.Database == "template1" || strings.HasPrefix(spec.Settings.Database, "pg_") || strings.HasPrefix(spec.Settings.Username, "pg_") {
			return fmt.Errorf("application %s: database and username must not use reserved PostgreSQL names", name)
		}
	}
	return nil
}

func (c ApplicationsConfig) names() []string {
	names := make([]string, 0, len(c.Apps))
	for name := range c.Apps {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// applicationChart packages the embedded recipe without network chart dependencies.
func applicationChart(recipe string) ([]byte, error) {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	err := fs.WalkDir(applications.Files, recipe, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := applications.Files.ReadFile(path)
		if err != nil {
			return err
		}
		if err = tw.WriteHeader(&tar.Header{Name: "ak3s-" + path, Mode: 0644, Size: int64(len(data))}); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err = tw.Close(); err != nil {
		return nil, err
	}
	if err = gz.Close(); err != nil {
		return nil, err
	}
	return archive.Bytes(), nil
}
