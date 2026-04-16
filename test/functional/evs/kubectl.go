//go:build functional

package evsfunctional

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type kubectl struct {
	kubeconfig string
}

func newKubectl(cfg testConfig) kubectl {
	return kubectl{kubeconfig: cfg.kubeconfig}
}

func (k kubectl) run(t *testing.T, args ...string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", k.kubeconfig}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}

	return string(output)
}

func (k kubectl) runInput(t *testing.T, input []byte, args ...string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", k.kubeconfig}, args...)...)
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}

	return string(output)
}

func (k kubectl) applyKustomize(t *testing.T, path string) {
	t.Helper()
	k.run(t, "apply", "-k", path)
}

func (k kubectl) deleteKustomize(t *testing.T, path string) {
	t.Helper()
	k.run(t, "delete", "-k", path, "--ignore-not-found=true")
}

func (k kubectl) createOrUpdateCloudSecret(t *testing.T, cfg testConfig) {
	t.Helper()

	args := []string{
		"-n", systemNamespace,
		"create", "secret", "generic", cloudSecretName,
		"--from-literal=OS_AUTH_URL=" + cfg.authURL,
		"--from-literal=OS_REGION=" + cfg.region,
		"--from-literal=OS_AVAILABILITY_ZONE=" + cfg.zone,
		"--from-literal=OS_USERNAME=" + cfg.username,
		"--from-literal=OS_PASSWORD=" + cfg.password,
		"--dry-run=client",
		"-o", "yaml",
	}
	if cfg.domainName != "" {
		args = append(args, "--from-literal=OS_DOMAIN_NAME="+cfg.domainName)
	}
	if cfg.projectID != "" {
		args = append(args, "--from-literal=OS_PROJECT_ID="+cfg.projectID)
	}
	if cfg.projectName != "" {
		args = append(args, "--from-literal=OS_PROJECT_NAME="+cfg.projectName)
	}

	manifest := k.run(t, args...)
	k.runInput(t, []byte(manifest), "apply", "-f", "-")
}

func (k kubectl) setDriverImage(t *testing.T, image string) {
	t.Helper()

	k.run(t, "-n", systemNamespace, "set", "image", "deployment/tcloud-public-csi-controller", "tcloud-public-csi-driver="+image)
	k.run(t, "-n", systemNamespace, "set", "image", "daemonset/tcloud-public-csi-node", "tcloud-public-csi-driver="+image)
}

func (k kubectl) waitForDriverReady(t *testing.T) {
	t.Helper()

	k.run(t, "-n", systemNamespace, "rollout", "status", "deployment/tcloud-public-csi-controller", "--timeout=15m")
	k.run(t, "-n", systemNamespace, "rollout", "status", "daemonset/tcloud-public-csi-node", "--timeout=20m")
	k.run(t, "-n", systemNamespace, "wait", "--for=condition=Ready", "pod", "-l", "app=tcloud-public-csi-controller", "--timeout=10m")
	k.run(t, "-n", systemNamespace, "wait", "--for=condition=Ready", "pod", "-l", "app=tcloud-public-csi-node", "--timeout=10m")
}

func (k kubectl) assertCSIDriverRegistered(t *testing.T) {
	t.Helper()

	output := strings.TrimSpace(k.run(t, "get", "csidriver", "csi.evs.tcloudpublic.com", "-o", "jsonpath={.metadata.name}"))
	if output != "csi.evs.tcloudpublic.com" {
		t.Fatalf("unexpected csidriver name: %q", output)
	}
}

func (k kubectl) collectDriverDebug(t *testing.T) {
	t.Helper()

	commands := [][]string{
		{"get", "pods", "-A", "-o", "wide"},
		{"-n", systemNamespace, "get", "pods", "-o", "wide"},
		{"-n", systemNamespace, "logs", "deployment/tcloud-public-csi-controller", "-c", "tcloud-public-csi-driver", "--tail=200"},
		{"-n", systemNamespace, "logs", "daemonset/tcloud-public-csi-node", "-c", "tcloud-public-csi-driver", "--tail=200"},
	}

	for _, args := range commands {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		cmd := exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", k.kubeconfig}, args...)...)
		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Logf("debug kubectl %s failed: %v", strings.Join(args, " "), err)
			continue
		}
		t.Logf("kubectl %s\n%s", strings.Join(args, " "), strings.TrimSpace(string(output)))
	}
}

func (k kubectl) describeResource(t *testing.T, resourceType, name string) {
	t.Helper()
	t.Logf("kubectl describe %s/%s\n%s", resourceType, name, k.run(t, "-n", systemNamespace, "describe", resourceType, name))
}

func (k kubectl) mustOutputContain(t *testing.T, description string, output string, want string) {
	t.Helper()
	if !strings.Contains(output, want) {
		t.Fatalf("%s did not contain %q: %s", description, want, output)
	}
}

func (k kubectl) ensureNamespaceExists(t *testing.T) {
	t.Helper()
	output := strings.TrimSpace(k.run(t, "get", "namespace", systemNamespace, "-o", "jsonpath={.metadata.name}"))
	if output != systemNamespace {
		t.Fatalf("namespace %s not found, got %q", systemNamespace, output)
	}
}
