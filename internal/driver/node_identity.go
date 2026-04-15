package driver

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"t-cloud-public-csi-driver/internal/config"
)

var providerIDUUIDPattern = regexp.MustCompile(`([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type nodeIDResolver interface {
	Resolve() (string, error)
}

type staticNodeIDResolver struct {
	nodeID string
	logger *slog.Logger
}

func (r *staticNodeIDResolver) Resolve() (string, error) {
	if r.logger != nil {
		r.logger.Info("using static node id", "node_id", r.nodeID)
	}
	return r.nodeID, nil
}

type kubeNodeIDResolver struct {
	nodeName string
	client   kubernetes.Interface
	logger   *slog.Logger

	once      sync.Once
	cachedID  string
	cachedErr error
}

func newNodeIDResolver(cfg config.Config) nodeIDResolver {
	logger := slog.Default().With("component", "node-id-resolver", "configured_node_id", cfg.NodeID)
	if isUUID(cfg.NodeID) {
		return &staticNodeIDResolver{nodeID: cfg.NodeID, logger: logger.With("source", "configured_uuid")}
	}

	client, err := newInClusterKubeClient()
	if err != nil {
		logger.Warn("failed to create in-cluster kubernetes client, falling back to configured node id", "error", err)
		return &staticNodeIDResolver{nodeID: cfg.NodeID, logger: logger.With("source", "configured_fallback")}
	}

	return &kubeNodeIDResolver{
		nodeName: cfg.NodeID,
		client:   client,
		logger:   logger.With("source", "kubernetes_node"),
	}
}

func isUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

func newInClusterKubeClient() (kubernetes.Interface, error) {
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(restConfig)
}

func (r *kubeNodeIDResolver) Resolve() (string, error) {
	r.once.Do(func() {
		r.cachedID, r.cachedErr = r.resolveOnce()
	})

	return r.cachedID, r.cachedErr
}

func (r *kubeNodeIDResolver) resolveOnce() (string, error) {
	node, err := r.client.CoreV1().Nodes().Get(context.Background(), r.nodeName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get kubernetes node %q: %w", r.nodeName, err)
	}

	instanceID, err := providerInstanceID(node)
	if err != nil {
		return "", err
	}
	if r.logger != nil {
		r.logger.Info(
			"resolved node instance id from kubernetes node",
			"kubernetes_node", r.nodeName,
			"provider_id", node.Spec.ProviderID,
			"system_uuid", node.Status.NodeInfo.SystemUUID,
			"instance_id", instanceID,
		)
	}

	return instanceID, nil
}

func providerInstanceID(node *corev1.Node) (string, error) {
	if node == nil {
		return "", fmt.Errorf("node is nil")
	}

	providerID := strings.TrimSpace(node.Spec.ProviderID)
	if providerID != "" {
		match := providerIDUUIDPattern.FindStringSubmatch(providerID)
		if len(match) == 2 {
			return strings.ToLower(match[1]), nil
		}
	}

	systemUUID := strings.TrimSpace(node.Status.NodeInfo.SystemUUID)
	if isUUID(systemUUID) {
		return strings.ToLower(systemUUID), nil
	}

	if providerID == "" {
		return "", fmt.Errorf("node %q does not have a usable spec.providerID or status.nodeInfo.systemUUID", node.Name)
	}

	return "", fmt.Errorf("node %q has unsupported providerID format %q and unusable systemUUID %q", node.Name, providerID, systemUUID)
}
