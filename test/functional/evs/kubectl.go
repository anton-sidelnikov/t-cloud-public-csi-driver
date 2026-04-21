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
	insecure   bool
}

func newKubectl(cfg testConfig) kubectl {
	return kubectl{kubeconfig: cfg.kubeconfig, insecure: cfg.insecureTLS}
}

func (k kubectl) run(t *testing.T, args ...string) string {
	t.Helper()

	output, err := k.runCommand(args...)
	if err == nil {
		return output
	}
	if k.shouldRetryInsecure(err, output) {
		t.Log("kubectl TLS verification failed, retrying with --insecure-skip-tls-verify=true")
		k.insecure = true
		output, err = k.runCommand(args...)
		if err == nil {
			return output
		}
	}
	t.Fatalf("kubectl %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(output))
	return ""
}

func (k kubectl) runInput(t *testing.T, input []byte, args ...string) string {
	t.Helper()

	output, err := k.runCommandInput(input, args...)
	if err == nil {
		return output
	}
	if k.shouldRetryInsecure(err, output) {
		t.Log("kubectl TLS verification failed, retrying with --insecure-skip-tls-verify=true")
		k.insecure = true
		output, err = k.runCommandInput(input, args...)
		if err == nil {
			return output
		}
	}
	t.Fatalf("kubectl %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(output))
	return ""
}

func (k kubectl) runCommand(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", k.commandArgs(args...)...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func (k kubectl) runCommandInput(input []byte, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", k.commandArgs(args...)...)
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func (k kubectl) commandArgs(args ...string) []string {
	commandArgs := []string{"--kubeconfig", k.kubeconfig}
	if k.insecure {
		commandArgs = append(commandArgs, "--insecure-skip-tls-verify=true")
	}
	commandArgs = append(commandArgs, args...)
	return commandArgs
}

func (k kubectl) shouldRetryInsecure(err error, output string) bool {
	if err == nil || k.insecure {
		return false
	}
	return strings.Contains(output, "x509: certificate signed by unknown authority")
}

func (k kubectl) applyKustomize(t *testing.T, path string) {
	t.Helper()
	k.run(t, "apply", "--validate=false", "-k", path)
}

func (k kubectl) deleteKustomize(t *testing.T, path string) {
	t.Helper()
	k.run(t, "delete", "--ignore-not-found=true", "-k", path)
}

func (k kubectl) applyManifest(t *testing.T, manifest string) {
	t.Helper()
	k.runInput(t, []byte(manifest), "apply", "--validate=false", "-f", "-")
}

func (k kubectl) deleteManifest(t *testing.T, manifest string) {
	t.Helper()
	k.runInput(t, []byte(manifest), "delete", "--ignore-not-found=true", "-f", "-")
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

	k.run(t, "-n", systemNamespace, "rollout", "status", "deployment/tcloud-public-csi-controller", "--timeout=10m")
	k.run(t, "-n", systemNamespace, "rollout", "status", "daemonset/tcloud-public-csi-node", "--timeout=10m")
	k.run(t, "-n", systemNamespace, "wait", "--for=condition=Ready", "pod", "-l", "app=tcloud-public-csi-controller", "--timeout=10m")
	k.run(t, "-n", systemNamespace, "wait", "--for=condition=Ready", "pod", "-l", "app=tcloud-public-csi-node", "--timeout=10m")
}

func (k kubectl) waitForSnapshotControllerReady(t *testing.T) {
	t.Helper()

	k.run(t, "-n", systemNamespace, "rollout", "status", "deployment/tcloud-public-snapshot-controller", "--timeout=10m")
	k.run(t, "-n", systemNamespace, "wait", "--for=condition=Ready", "pod", "-l", "app=tcloud-public-snapshot-controller", "--timeout=10m")
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
		{"-n", systemNamespace, "get", "deployment", "tcloud-public-snapshot-controller", "-o", "wide"},
		{"-n", systemNamespace, "logs", "deployment/tcloud-public-csi-controller", "-c", "tcloud-public-csi-driver", "--tail=200"},
		{"-n", systemNamespace, "logs", "daemonset/tcloud-public-csi-node", "-c", "tcloud-public-csi-driver", "--tail=200"},
		{"-n", systemNamespace, "logs", "deployment/tcloud-public-snapshot-controller", "--tail=200"},
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

func (k kubectl) collectNamespaceDebug(t *testing.T, namespace string) {
	t.Helper()

	commands := [][]string{
		{"-n", namespace, "get", "all", "-o", "wide"},
		{"-n", namespace, "get", "pvc,pv", "-o", "wide"},
		{"-n", namespace, "get", "volumesnapshots,volumesnapshotcontents", "-o", "wide"},
	}

	for _, args := range commands {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		cmd := exec.CommandContext(ctx, "kubectl", append(k.commandArgs(), args...)...)
		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Logf("debug kubectl %s failed: %v", strings.Join(args, " "), err)
			continue
		}
		t.Logf("kubectl %s\n%s", strings.Join(args, " "), strings.TrimSpace(string(output)))
	}
}

func (k kubectl) waitForPVCBound(t *testing.T, namespace, name string) {
	t.Helper()
	k.run(t, "-n", namespace, "wait", "--for=jsonpath={.status.phase}=Bound", "pvc/"+name, "--timeout=5m")
}

func (k kubectl) waitForVolumeSnapshotReady(t *testing.T, namespace, name string) {
	t.Helper()
	k.run(t, "-n", namespace, "wait", "--for=jsonpath={.status.readyToUse}=true", "volumesnapshot/"+name, "--timeout=10m")
}

func (k kubectl) waitForPodReady(t *testing.T, namespace, name string) {
	t.Helper()
	k.run(t, "-n", namespace, "wait", "--for=condition=Ready", "pod/"+name, "--timeout=5m")
}

func (k kubectl) getNamespacedJSONPath(t *testing.T, namespace, resource, jsonpath string) string {
	t.Helper()
	return strings.TrimSpace(k.run(t, "-n", namespace, "get", resource, "-o", "jsonpath="+jsonpath))
}

func (k kubectl) execInPod(t *testing.T, namespace, pod string, command ...string) string {
	t.Helper()

	args := []string{"-n", namespace, "exec", pod, "--"}
	args = append(args, command...)
	return k.run(t, args...)
}

func (k kubectl) hasVolumeSnapshotCRDs(t *testing.T) bool {
	t.Helper()

	required := []string{
		"crd/volumesnapshots.snapshot.storage.k8s.io",
		"crd/volumesnapshotcontents.snapshot.storage.k8s.io",
		"crd/volumesnapshotclasses.snapshot.storage.k8s.io",
	}
	for _, resource := range required {
		if _, err := k.runCommand("get", resource); err != nil {
			return false
		}
	}

	return true
}

func (k kubectl) snapshotControllerExists(t *testing.T) bool {
	t.Helper()

	_, err := k.runCommand("-n", systemNamespace, "get", "deployment", "tcloud-public-snapshot-controller")
	return err == nil
}

func (k kubectl) createNamespace(t *testing.T, namespace string) {
	t.Helper()

	manifest := `apiVersion: v1
kind: Namespace
metadata:
  name: ` + namespace + "\n"
	k.applyManifest(t, manifest)
}

func (k kubectl) deleteNamespace(t *testing.T, namespace string) {
	t.Helper()
	k.run(t, "delete", "namespace", namespace, "--ignore-not-found=true", "--wait=false")
}

func (k kubectl) deletePod(t *testing.T, namespace, name string) {
	t.Helper()
	k.run(t, "-n", namespace, "delete", "pod", name, "--ignore-not-found=true", "--wait=false")
}

func (k kubectl) deletePvc(t *testing.T, namespace, name string) {
	t.Helper()
	k.run(t, "-n", namespace, "delete", "pvc", name, "--ignore-not-found=true", "--wait=false")
}

func (k kubectl) deleteVolumeSnapshot(t *testing.T, namespace, name string) {
	t.Helper()
	k.run(t, "-n", namespace, "delete", "volumesnapshot", name, "--ignore-not-found=true", "--wait=false")
}

func testNamespace(t *testing.T) string {
	t.Helper()

	name := strings.ToLower(t.Name())
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.Trim(name, "-")
	if len(name) > 40 {
		name = name[:40]
	}

	return "tcloud-csi-e2e-" + name
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
