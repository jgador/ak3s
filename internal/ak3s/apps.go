package ak3s

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const applicationManagedLabel = "ak3s.io/managed-by"
const applicationNameLabel = "ak3s.io/application"

type preparedApplication struct {
	Name, Release, Namespace string
	Recipe                   applicationRecipe
	Spec                     ApplicationSpec
	PVC, Inventory           []byte
	Observed                 []byte
	HasInventory             bool
}

// ApplicationBundle owns prepared Helm artifacts and the ordered application plan.
type ApplicationBundle struct {
	*Bundle
	Applications []preparedApplication
}

// ApplicationPreparer validates optional workloads before cluster changes.
type ApplicationPreparer interface {
	PrepareApplications(context.Context, ApplicationsConfig, Pins, Snapshot, bool) (*ApplicationBundle, error)
}

func applicationLabels(name string) map[string]string {
	return map[string]string{applicationManagedLabel: "ak3s", applicationNameLabel: name}
}

func applicationResource(kind, name string, app preparedApplication) map[string]any {
	return map[string]any{"apiVersion": "v1", "kind": kind, "metadata": map[string]any{
		"name": name, "namespace": app.Namespace, "labels": applicationLabels(app.Name),
	}}
}

func applicationPVC(app preparedApplication) ([]byte, error) {
	resource := applicationResource("PersistentVolumeClaim", app.Release+"-data", app)
	resource["spec"] = map[string]any{"accessModes": []string{"ReadWriteOnce"}, "storageClassName": app.Spec.Settings.StorageClass,
		"resources": map[string]any{"requests": map[string]string{"storage": app.Spec.Settings.Storage}}}
	return yaml.Marshal(resource)
}

func applicationInventory(app preparedApplication) ([]byte, error) {
	return applicationInventoryForPhase(app, "ready")
}

func applicationInventoryForPhase(app preparedApplication, phase string) ([]byte, error) {
	resource := applicationResource("ConfigMap", app.Release, app)
	spec, err := json.Marshal(app.Spec)
	if err != nil {
		return nil, err
	}
	resource["data"] = map[string]string{"spec": string(spec), "recipe_version": app.Recipe.Version,
		"postgres_version": app.Recipe.PostgresVersion, "image": app.Recipe.Image, "phase": phase}
	return yaml.Marshal(resource)
}

func applicationKube(ctx context.Context, runner Runner, b *Bundle, input []byte, args ...string) ([]byte, error) {
	return runner.Run(ctx, Command{Name: K3sPath, Args: append([]string{"kubectl", "--kubeconfig", KubeconfigPath,
		"--cache-dir", filepath.Join(b.Dir, "kube-cache")}, args...), Input: input})
}

func observeApplication(ctx context.Context, runner Runner, b *Bundle, app preparedApplication) ([]byte, error) {
	return applicationKube(ctx, runner, b, nil, "get", "configmap/"+app.Release, "persistentvolumeclaim/"+app.Release+"-data",
		"statefulset/"+app.Release, "service/"+app.Release, "service/"+app.Release+"-headless", "--namespace", app.Namespace,
		"--ignore-not-found", "--output=json", "--request-timeout=30s")
}

type applicationObject struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name        string            `json:"name"`
		UID         string            `json:"uid"`
		Annotations map[string]string `json:"annotations"`
		Labels      map[string]string `json:"labels"`
	} `json:"metadata"`
	Data map[string]string `json:"data"`
	Spec struct {
		Template         json.RawMessage `json:"template"`
		StorageClassName string          `json:"storageClassName"`
		AccessModes      []string        `json:"accessModes"`
		Resources        struct {
			Requests map[string]string `json:"requests"`
		} `json:"resources"`
	} `json:"spec"`
}

func applicationObjects(observed []byte) ([]applicationObject, error) {
	if len(bytes.TrimSpace(observed)) == 0 {
		return nil, nil
	}
	var list struct {
		Kind  string              `json:"kind"`
		Items []applicationObject `json:"items"`
	}
	if err := json.Unmarshal(observed, &list); err != nil || list.Kind != "List" {
		return nil, errors.New("invalid application resource response")
	}
	return list.Items, nil
}

func storageBytes(quantity string) (uint64, error) {
	for suffix, scale := range map[string]uint64{"Mi": 1 << 20, "Gi": 1 << 30, "Ti": 1 << 40} {
		if strings.HasSuffix(quantity, suffix) {
			n, err := strconv.ParseUint(strings.TrimSuffix(quantity, suffix), 10, 64)
			if err != nil || n == 0 || n > ^uint64(0)/scale {
				return 0, errors.New("invalid storage quantity")
			}
			return n * scale, nil
		}
	}
	return 0, errors.New("unsupported storage quantity")
}

func checkApplicationResources(app preparedApplication, upgrade, releaseExists bool) error {
	objects, err := applicationObjects(app.Observed)
	if err != nil {
		return err
	}
	var inventory *applicationObject
	hasPVC := false
	for i := range objects {
		object := &objects[i]
		if object.Metadata.Labels[applicationManagedLabel] != "ak3s" || object.Metadata.Labels[applicationNameLabel] != app.Name {
			return errors.New("an application destination is already owned by another installation")
		}
		if object.Kind == "ConfigMap" && object.Metadata.Name == app.Release {
			inventory = object
		}
		if object.Kind == "PersistentVolumeClaim" {
			hasPVC = true
			requested, err := storageBytes(app.Spec.Settings.Storage)
			if err != nil {
				return err
			}
			existing, err := storageBytes(object.Spec.Resources.Requests["storage"])
			if err != nil || requested != existing || object.Spec.StorageClassName != app.Spec.Settings.StorageClass ||
				len(object.Spec.AccessModes) != 1 || object.Spec.AccessModes[0] != "ReadWriteOnce" {
				return errors.New("PVC size, storage class, and access mode cannot change implicitly; migrate storage separately")
			}
		}
	}
	if inventory == nil {
		if releaseExists || len(objects) > 0 {
			return errors.New("existing application resources have no AK3S inventory; explicit migration is required")
		}
		return nil
	}
	if inventory.Data["phase"] != "ready" && inventory.Data["phase"] != "provisioning" {
		return errors.New("invalid application inventory phase; restore it before continuing")
	}
	if !hasPVC && (len(objects) > 1 || inventory.Data["phase"] == "ready") {
		return errors.New("application storage is missing beside existing workloads; restore its data before continuing")
	}
	if !hasPVC && releaseExists {
		return errors.New("installed application PVC is missing; restore its data before continuing")
	}
	var previous ApplicationSpec
	if err = json.Unmarshal([]byte(inventory.Data["spec"]), &previous); err != nil {
		return errors.New("invalid application inventory; restore it before continuing")
	}
	oldStorage, oldErr := storageBytes(previous.Settings.Storage)
	newStorage, newErr := storageBytes(app.Spec.Settings.Storage)
	if oldErr != nil || newErr != nil || oldStorage != newStorage || previous.Recipe != app.Spec.Recipe || previous.Namespace != app.Namespace ||
		previous.Settings.StorageClass != app.Spec.Settings.StorageClass || previous.Settings.Database != app.Spec.Settings.Database ||
		previous.Settings.Username != app.Spec.Settings.Username || previous.Settings.ExistingSecret != app.Spec.Settings.ExistingSecret {
		return errors.New("application storage, database identity, and credential reference cannot change implicitly")
	}
	oldVersion := inventory.Data["postgres_version"]
	if oldVersion == "" || inventory.Data["recipe_version"] == "" || inventory.Data["image"] == "" {
		return errors.New("application inventory is missing pinned versions")
	}
	if strings.Split(oldVersion, ".")[0] != strings.Split(app.Recipe.PostgresVersion, ".")[0] {
		return errors.New("PostgreSQL major version changes require a database migration")
	}
	oldMinor, err := strconv.Atoi(strings.TrimPrefix(oldVersion, strings.Split(oldVersion, ".")[0]+"."))
	newMinor, newErr := strconv.Atoi(strings.TrimPrefix(app.Recipe.PostgresVersion, strings.Split(app.Recipe.PostgresVersion, ".")[0]+"."))
	if err != nil || newErr != nil || oldMinor > newMinor {
		return errors.New("PostgreSQL downgrades and invalid installed versions are refused")
	}
	if !upgrade && (inventory.Data["image"] != app.Recipe.Image || inventory.Data["recipe_version"] != app.Recipe.Version) {
		return errors.New("application version changed; back up the database and run ak3s apps upgrade")
	}
	return nil
}

// PrepareApplications renders embedded recipes using the verified Helm binary.
// A connected server receives ownership, PVC, credential, and upgrade checks before writes.
func (n NativePreparer) PrepareApplications(ctx context.Context, c ApplicationsConfig, pins Pins, s Snapshot, upgrade bool) (result *ApplicationBundle, err error) {
	if err = c.Validate(); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "ak3s-apps-")
	if err != nil {
		return nil, err
	}
	b := &Bundle{Dir: dir, Values: map[string][]byte{}, Charts: map[string][]byte{}, Rendered: map[string][]byte{}, Manifests: map[string][]byte{}, Releases: map[string]string{}}
	result = &ApplicationBundle{Bundle: b}
	defer func() {
		if err != nil {
			b.Close()
		}
	}()
	if len(c.Apps) == 0 {
		return result, nil
	}
	if err = n.prepareHelm(ctx, b, pins, s.Arch); err != nil {
		return nil, err
	}
	connected := s.Managed && s.ServiceActive && s.Kubeconfig
	if connected {
		out, runErr := n.Runner.Run(ctx, Command{Name: b.Helm, Args: []string{"list", "--all-namespaces", "--all", "--output=json", "--kubeconfig", KubeconfigPath}, Env: b.HelmEnv()})
		if runErr != nil {
			return nil, runErr
		}
		var releases []struct{ Name, Namespace string }
		if err = json.Unmarshal(out, &releases); err != nil {
			return nil, errors.New("invalid Helm release response")
		}
		for _, release := range releases {
			b.Releases[release.Namespace+"/"+release.Name] = "installed"
		}
	}
	catalog, err := loadApplicationCatalog()
	if err != nil {
		return nil, err
	}
	for _, name := range c.names() {
		spec := c.Apps[name]
		app := preparedApplication{Name: name, Release: "ak3s-app-" + name, Namespace: spec.Namespace, Recipe: catalog[spec.Recipe], Spec: spec}
		_, installed := b.Releases[app.Namespace+"/"+app.Release]
		if connected {
			app.Observed, err = observeApplication(ctx, n.Runner, b, app)
			if err != nil {
				return nil, fmt.Errorf("inspect application %s: %w", name, err)
			}
			if err = checkApplicationResources(app, upgrade, installed); err != nil {
				return nil, fmt.Errorf("application %s: %w", name, err)
			}
			objects, _ := applicationObjects(app.Observed)
			for _, object := range objects {
				app.HasInventory = app.HasInventory || object.Kind == "ConfigMap"
			}
			// Check only presence and key length; never fetch or print the Secret value.
			secret, runErr := applicationKube(ctx, n.Runner, b, nil, "get", "secret", spec.Settings.ExistingSecret, "--namespace", app.Namespace,
				"--output=go-template={{if .data.password}}{{len .data.password}}{{end}}", "--request-timeout=30s")
			if runErr != nil || len(bytes.TrimSpace(secret)) == 0 || strings.TrimSpace(string(secret)) == "0" {
				return nil, fmt.Errorf("application %s: existing_secret must exist in its namespace with a nonempty password key", name)
			}
		}
		chart := Chart{Release: app.Release, Namespace: app.Namespace, SkipCRDs: true}
		archive, err := applicationChart(spec.Recipe)
		if err != nil {
			return nil, err
		}
		values, err := yaml.Marshal(map[string]string{"application": name, "image": app.Recipe.Image, "database": spec.Settings.Database,
			"username": spec.Settings.Username, "existingSecret": spec.Settings.ExistingSecret})
		if err != nil {
			return nil, err
		}
		b.Values[app.Release], b.Charts[app.Release] = values, archive
		if err = os.WriteFile(b.ChartPath(chart), archive, 0600); err != nil {
			return nil, err
		}
		if err = os.WriteFile(b.ValuesPath(chart), values, 0600); err != nil {
			return nil, err
		}
		version := strings.Split(strings.TrimPrefix(s.K3sVersion, "v"), "+")[0]
		if s.K3sVersion == "" {
			version = kubeVersion(pins)
		}
		args := []string{"template", app.Release, b.ChartPath(chart), "--namespace", app.Namespace, "--kube-version", version, "--values", b.ValuesPath(chart)}
		if installed {
			args = append(args, "--is-upgrade")
		}
		rendered, err := n.Runner.Run(ctx, Command{Name: b.Helm, Args: args, Env: b.HelmEnv()})
		if err != nil {
			return nil, fmt.Errorf("render application %s: %w", name, err)
		}
		if err = ValidateManifests(rendered); err != nil {
			return nil, err
		}
		b.Rendered[app.Release] = rendered
		app.PVC, err = applicationPVC(app)
		if err != nil {
			return nil, err
		}
		app.Inventory, err = applicationInventory(app)
		if err != nil {
			return nil, err
		}
		b.Manifests[app.Release+"-pvc.yaml"], b.Manifests[app.Release+"-inventory.yaml"] = app.PVC, app.Inventory
		if connected {
			if installed {
				args := append(helmUpgradeArgs(chart, b), "--dry-run=server")
				if _, err = n.Runner.Run(ctx, Command{Name: b.Helm, Args: args, Env: b.HelmEnv()}); err != nil {
					return nil, fmt.Errorf("validate application %s: %w", name, err)
				}
			}
			for _, manifest := range [][]byte{app.PVC, app.Inventory} {
				if _, err = applicationKube(ctx, n.Runner, b, manifest, "apply", "--dry-run=server", "-f", "-"); err != nil {
					return nil, fmt.Errorf("validate application %s resources: %w", name, err)
				}
			}
		}
		result.Applications = append(result.Applications, app)
	}
	return result, nil
}

func applyApplications(ctx context.Context, runner Runner, b *ApplicationBundle) error {
	for _, app := range b.Applications {
		observed, err := observeApplication(ctx, runner, b.Bundle, app)
		if err != nil {
			return err
		}
		// Ignore changing pod status, but require the destinations to remain unchanged.
		before, err := applicationObjects(app.Observed)
		if err != nil {
			return err
		}
		after, err := applicationObjects(observed)
		if err != nil {
			return err
		}
		oldJSON, _ := json.Marshal(before)
		newJSON, _ := json.Marshal(after)
		if !bytes.Equal(oldJSON, newJSON) {
			return fmt.Errorf("application %s changed while planning; rerun AK3S", app.Name)
		}
	}
	for _, app := range b.Applications {
		// The inventory is a checkpoint before installation; retries keep the same identity.
		// The PVC is outside Helm so an atomic rollback cannot delete database storage.
		resources := [][]byte{app.PVC}
		if !app.HasInventory {
			checkpoint, err := applicationInventoryForPhase(app, "provisioning")
			if err != nil {
				return err
			}
			resources = append([][]byte{checkpoint}, resources...)
		}
		for _, resource := range resources {
			if _, err := applicationKube(ctx, runner, b.Bundle, resource, "apply", "-f", "-"); err != nil {
				return fmt.Errorf("apply application %s resources: %w; fix the error and rerun", app.Name, err)
			}
		}
		chart := Chart{Release: app.Release, Namespace: app.Namespace, SkipCRDs: true}
		if _, err := runner.Run(ctx, Command{Name: b.Helm, Args: helmUpgradeArgs(chart, b.Bundle), Env: b.HelmEnv()}); err != nil {
			return fmt.Errorf("reconcile application %s: %w; the PVC is retained, fix the error and rerun", app.Name, err)
		}
		if _, err := applicationKube(ctx, runner, b.Bundle, nil, "rollout", "status", "statefulset/"+app.Release, "--namespace", app.Namespace, "--timeout=300s"); err != nil {
			return fmt.Errorf("wait for application %s: %w", app.Name, err)
		}
		if _, err := applicationKube(ctx, runner, b.Bundle, nil, "wait", "--for=jsonpath={.status.phase}=Bound", "persistentvolumeclaim/"+app.Release+"-data", "--namespace", app.Namespace, "--timeout=180s"); err != nil {
			return fmt.Errorf("wait for application %s PVC: %w", app.Name, err)
		}
		if _, err := applicationKube(ctx, runner, b.Bundle, app.Inventory, "apply", "-f", "-"); err != nil {
			return fmt.Errorf("record application %s: %w; rerun the same application command", app.Name, err)
		}
	}
	return nil
}
