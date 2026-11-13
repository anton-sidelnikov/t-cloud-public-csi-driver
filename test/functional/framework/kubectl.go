//go:build functional

package framework

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Kubectl runs kubectl against the functional test cluster.
type Kubectl struct {
	kubeconfig string
	insecure   bool
}

func NewKubectl(cfg Config) *Kubectl {
	return &Kubectl{kubeconfig: cfg.Kubeconfig, insecure: cfg.InsecureTLS}
}

// Run executes kubectl and fails the test on error.
func (k *Kubectl) Run(t *testing.T, args ...string) string {
	t.Helper()
	return k.mustRun(t, nil, args...)
}

// RunInput executes kubectl with stdin and fails the test on error.
func (k *Kubectl) RunInput(t *testing.T, input []byte, args ...string) string {
	t.Helper()
	return k.mustRun(t, input, args...)
}

func (k *Kubectl) mustRun(t *testing.T, input []byte, args ...string) string {
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

// RunCommand executes kubectl and returns its error instead of failing the test.
func (k *Kubectl) RunCommand(args ...string) (string, error) {
	return k.runCommandInput(nil, args...)
}

func (k *Kubectl) runCommandInput(input []byte, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", k.commandArgs(args...)...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func (k *Kubectl) commandArgs(args ...string) []string {
	commandArgs := []string{"--kubeconfig", k.kubeconfig}
	if k.insecure {
		commandArgs = append(commandArgs, "--insecure-skip-tls-verify=true")
	}
	return append(commandArgs, args...)
}

func (k *Kubectl) shouldRetryInsecure(err error, output string) bool {
	if err == nil || k.insecure {
		return false
	}
	return strings.Contains(output, "x509: certificate signed by unknown authority")
}

func (k *Kubectl) ApplyKustomize(t *testing.T, path string) {
	t.Helper()
	k.Run(t, "apply", "--validate=false", "-k", path)
}

func (k *Kubectl) ApplyManifest(t *testing.T, manifest string) {
	t.Helper()
	k.RunInput(t, []byte(manifest), "apply", "--validate=false", "-f", "-")
}

func (k *Kubectl) DeploymentExists(namespace, name string) bool {
	_, err := k.RunCommand("-n", namespace, "get", "deployment", name)
	return err == nil
}

func (k *Kubectl) ScaleDeployment(t *testing.T, namespace, name string, replicas int) {
	t.Helper()
	k.Run(t, "-n", namespace, "scale", "deployment", name, fmt.Sprintf("--replicas=%d", replicas))
}

func (k *Kubectl) WaitForDeploymentReplicas(t *testing.T, namespace, name string, replicas int) {
	t.Helper()

	want := strconv.Itoa(replicas)
	var output string
	Eventually(t, 10*time.Minute, 3*time.Second, func() bool {
		output = strings.TrimSpace(k.Run(t, "-n", namespace, "get", "deployment", name, "-o", "jsonpath={.status.readyReplicas}"))
		return output == want || (output == "" && replicas == 0)
	}, func() string {
		return fmt.Sprintf("deployment %s/%s did not reach %d ready replicas, got %q", namespace, name, replicas, output)
	})
}

// LogCommands runs diagnostic kubectl commands and logs their output, ignoring failures.
func (k *Kubectl) LogCommands(t *testing.T, commands ...[]string) {
	t.Helper()

	for _, args := range commands {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		cmd := exec.CommandContext(ctx, "kubectl", k.commandArgs(args...)...)
		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Logf("debug kubectl %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
			continue
		}
		t.Logf("kubectl %s\n%s", strings.Join(args, " "), strings.TrimSpace(string(output)))
	}
}

func (k *Kubectl) CollectNamespaceDebug(t *testing.T, namespace string) {
	t.Helper()

	k.LogCommands(t,
		[]string{"-n", namespace, "get", "all", "-o", "wide"},
		[]string{"-n", namespace, "get", "pvc,pv", "-o", "wide"},
		[]string{"-n", namespace, "get", "volumesnapshots,volumesnapshotcontents", "-o", "wide"},
		[]string{"-n", namespace, "get", "events", "--sort-by=.metadata.creationTimestamp"},
		[]string{"get", "volumeattachments", "-o", "wide"},
	)
}

func (k *Kubectl) WaitForPVCBound(t *testing.T, namespace, name string, timeout time.Duration) {
	t.Helper()
	k.Run(t, "-n", namespace, "wait", "--for=jsonpath={.status.phase}=Bound", "pvc/"+name, "--timeout="+timeout.String())
}

func (k *Kubectl) WaitForVolumeSnapshotReady(t *testing.T, namespace, name string) {
	t.Helper()
	k.Run(t, "-n", namespace, "wait", "--for=jsonpath={.status.readyToUse}=true", "volumesnapshot/"+name, "--timeout=10m")
}

func (k *Kubectl) WaitForPodReady(t *testing.T, namespace, name string) {
	t.Helper()
	k.Run(t, "-n", namespace, "wait", "--for=condition=Ready", "pod/"+name, "--timeout=5m")
}

func (k *Kubectl) WaitForPodDeleted(t *testing.T, namespace, name string) {
	t.Helper()
	k.Run(t, "-n", namespace, "wait", "--for=delete", "pod/"+name, "--timeout=5m")
}

// WaitForPVDeleted waits until the PV is gone, which means the CSI DeleteVolume call succeeded.
func (k *Kubectl) WaitForPVDeleted(t *testing.T, name string, timeout time.Duration) {
	t.Helper()
	k.Run(t, "wait", "--for=delete", "pv/"+name, "--timeout="+timeout.String())
}

func (k *Kubectl) GetNamespacedJSONPath(t *testing.T, namespace, resource, jsonpath string) string {
	t.Helper()
	return strings.TrimSpace(k.Run(t, "-n", namespace, "get", resource, "-o", "jsonpath="+jsonpath))
}

func (k *Kubectl) ExecInPod(t *testing.T, namespace, pod string, command ...string) string {
	t.Helper()
	return k.Run(t, append([]string{"-n", namespace, "exec", pod, "--"}, command...)...)
}

func (k *Kubectl) HasVolumeSnapshotCRDs() bool {
	for _, resource := range []string{
		"crd/volumesnapshots.snapshot.storage.k8s.io",
		"crd/volumesnapshotcontents.snapshot.storage.k8s.io",
		"crd/volumesnapshotclasses.snapshot.storage.k8s.io",
	} {
		if _, err := k.RunCommand("get", resource); err != nil {
			return false
		}
	}
	return true
}

func (k *Kubectl) CreateNamespace(t *testing.T, namespace string) {
	t.Helper()
	k.ApplyManifest(t, "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: "+namespace+"\n")
}

func (k *Kubectl) DeleteNamespace(t *testing.T, namespace string) {
	t.Helper()
	k.Run(t, "delete", "namespace", namespace, "--ignore-not-found=true", "--wait=false")
}

func (k *Kubectl) DeletePod(t *testing.T, namespace, name string) {
	t.Helper()
	k.Run(t, "-n", namespace, "delete", "pod", name, "--ignore-not-found=true", "--wait=false")
}

func (k *Kubectl) DeletePVC(t *testing.T, namespace, name string) {
	t.Helper()
	k.Run(t, "-n", namespace, "delete", "pvc", name, "--ignore-not-found=true", "--wait=false")
}

func (k *Kubectl) DeleteVolumeSnapshot(t *testing.T, namespace, name string) {
	t.Helper()
	k.Run(t, "-n", namespace, "delete", "volumesnapshot", name, "--ignore-not-found=true", "--wait=false")
}

// TestNamespace derives a per-test namespace name from the test name.
func TestNamespace(t *testing.T) string {
	t.Helper()

	name := strings.ToLower(t.Name())
	name = strings.NewReplacer("/", "-", "_", "-", " ", "-").Replace(name)
	name = strings.Trim(name, "-")
	if len(name) > 40 {
		name = name[:40]
	}

	return "tcloud-csi-e2e-" + name
}

// MustContain fails the test when output does not contain want.
func MustContain(t *testing.T, description, output, want string) {
	t.Helper()
	if !strings.Contains(output, want) {
		t.Fatalf("%s did not contain %q: %s", description, want, output)
	}
}

// Eventually polls cond until it returns true or the timeout expires.
func Eventually(t *testing.T, timeout, interval time.Duration, cond func() bool, failure func() string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(failure())
		}
		time.Sleep(interval)
	}
}
