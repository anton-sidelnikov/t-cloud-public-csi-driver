package driver

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestProviderInstanceIDExtractsUUID(t *testing.T) {
	node := &corev1.Node{}
	node.Name = "worker-1"
	node.Spec.ProviderID = "openstack:///123E4567-E89B-12D3-A456-426614174000"

	got, err := providerInstanceID(node)
	if err != nil {
		t.Fatalf("providerInstanceID returned error: %v", err)
	}
	if got != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("unexpected instance id: %q", got)
	}
}

func TestProviderInstanceIDRejectsMissingProviderID(t *testing.T) {
	node := &corev1.Node{}
	node.Name = "worker-1"

	_, err := providerInstanceID(node)
	if err == nil {
		t.Fatal("expected error for missing providerID")
	}
}

func TestProviderInstanceIDRejectsUnsupportedFormat(t *testing.T) {
	node := &corev1.Node{}
	node.Name = "worker-1"
	node.Spec.ProviderID = "openstack:///not-a-uuid"

	_, err := providerInstanceID(node)
	if err == nil {
		t.Fatal("expected error for unsupported providerID format")
	}
}
