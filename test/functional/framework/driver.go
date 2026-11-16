//go:build functional

package framework

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

const (
	// CloudSecretName is the secret consumed by every driver deployment.
	CloudSecretName = "tcloud-public-csi-cloud-config"
	driverContainer = "tcloud-public-csi-driver"
)

// Driver describes one deployed CSI backend.
type Driver struct {
	// Name is the CSIDriver name.
	Name string
	// DeployPath is the kustomization directory applied by Install.
	DeployPath string
	Namespace  string
	Controller string
	Node       string
	// ExtraSecret holds backend-specific keys added to the cloud secret.
	ExtraSecret map[string]string
}

// Install applies the manifests, writes the cloud secret, switches to the test image, and waits for readiness.
func (d Driver) Install(t *testing.T, cfg Config, k *Kubectl) {
	t.Helper()

	t.Logf("step: apply CSI manifests from %s", d.DeployPath)
	k.ApplyKustomize(t, d.DeployPath)
	t.Logf("step: verify namespace %s exists", d.Namespace)
	if got := strings.TrimSpace(k.Run(t, "get", "namespace", d.Namespace, "-o", "jsonpath={.metadata.name}")); got != d.Namespace {
		t.Fatalf("namespace %s not found, got %q", d.Namespace, got)
	}
	t.Logf("step: create or update cloud secret %s/%s", d.Namespace, CloudSecretName)
	d.ApplyCloudSecret(t, k, cfg, cfg.Zone)
	t.Logf("step: patch controller and node images to %s", cfg.DriverImage)
	k.Run(t, "-n", d.Namespace, "set", "image", "deployment/"+d.Controller, driverContainer+"="+cfg.DriverImage)
	k.Run(t, "-n", d.Namespace, "set", "image", "daemonset/"+d.Node, driverContainer+"="+cfg.DriverImage)
	t.Log("step: wait for controller and node rollout readiness")
	d.WaitReady(t, k)
	t.Logf("step: verify CSIDriver %s registration", d.Name)
	if got := strings.TrimSpace(k.Run(t, "get", "csidriver", d.Name, "-o", "jsonpath={.metadata.name}")); got != d.Name {
		t.Fatalf("unexpected csidriver name: %q", got)
	}
}

// ApplyCloudSecret writes the cloud secret; an empty zone omits OS_AVAILABILITY_ZONE.
func (d Driver) ApplyCloudSecret(t *testing.T, k *Kubectl, cfg Config, zone string) {
	t.Helper()

	values := map[string]string{
		"OS_AUTH_URL":          cfg.AuthURL,
		"OS_REGION":            cfg.Region,
		"OS_USERNAME":          cfg.Username,
		"OS_PASSWORD":          cfg.Password,
		"OS_AVAILABILITY_ZONE": zone,
		"OS_DOMAIN_NAME":       cfg.DomainName,
		"OS_PROJECT_ID":        cfg.ProjectID,
		"OS_PROJECT_NAME":      cfg.ProjectName,
	}
	maps.Copy(values, d.ExtraSecret)

	args := []string{"-n", d.Namespace, "create", "secret", "generic", CloudSecretName, "--dry-run=client", "-o", "yaml"}
	for _, key := range slices.Sorted(maps.Keys(values)) {
		if values[key] != "" {
			args = append(args, "--from-literal="+key+"="+values[key])
		}
	}

	manifest := k.Run(t, args...)
	k.RunInput(t, []byte(manifest), "apply", "-f", "-")
}

// Restart restarts the controller and node workloads and waits for readiness.
func (d Driver) Restart(t *testing.T, k *Kubectl) {
	t.Helper()

	k.Run(t, "-n", d.Namespace, "rollout", "restart", "deployment/"+d.Controller)
	k.Run(t, "-n", d.Namespace, "rollout", "restart", "daemonset/"+d.Node)
	d.WaitReady(t, k)
}

// WaitReady waits for the controller and node rollouts.
func (d Driver) WaitReady(t *testing.T, k *Kubectl) {
	t.Helper()

	// rollout status already waits for the new pods to become available. A label-based
	// "kubectl wait pod" would also select old pods that are still terminating and then
	// block until the timeout once they are deleted.
	k.Run(t, "-n", d.Namespace, "rollout", "status", "deployment/"+d.Controller, "--timeout=10m")
	k.Run(t, "-n", d.Namespace, "rollout", "status", "daemonset/"+d.Node, "--timeout=10m")
}

// NodeLogs returns the driver container logs of all node plugin pods.
func (d Driver) NodeLogs(k *Kubectl) (string, error) {
	return k.RunCommand("-n", d.Namespace, "logs", "-l", "app="+d.Node, "-c", driverContainer, "--tail=-1")
}

// NodeNames returns the nodes that run a node plugin pod.
func (d Driver) NodeNames(t *testing.T, k *Kubectl) []string {
	t.Helper()
	return strings.Fields(k.Run(t, "-n", d.Namespace, "get", "pods", "-l", "app="+d.Node, "-o", "jsonpath={.items[*].spec.nodeName}"))
}

// CollectDebug logs the driver pods and driver container logs.
func (d Driver) CollectDebug(t *testing.T, k *Kubectl) {
	t.Helper()

	k.LogCommands(t,
		[]string{"-n", d.Namespace, "get", "pods", "-o", "wide"},
		[]string{"-n", d.Namespace, "logs", "deployment/" + d.Controller, "-c", driverContainer, "--tail=200"},
		[]string{"-n", d.Namespace, "logs", "daemonset/" + d.Node, "-c", driverContainer, "--tail=200"},
	)
}
