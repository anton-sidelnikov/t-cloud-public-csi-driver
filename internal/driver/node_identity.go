package driver

import (
	"context"
	"fmt"
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
}

func (r *staticNodeIDResolver) Resolve() (string, error) {
	return r.nodeID, nil
}

type kubeNodeIDResolver struct {
	nodeName string
	client   kubernetes.Interface

	once      sync.Once
	cachedID  string
	cachedErr error
}

func newNodeIDResolver(cfg config.Config) nodeIDResolver {
	if isUUID(cfg.NodeID) {
		return &staticNodeIDResolver{nodeID: cfg.NodeID}
	}

	client, err := newInClusterKubeClient()
	if err != nil {
		return &staticNodeIDResolver{nodeID: cfg.NodeID}
	}

	return &kubeNodeIDResolver{
		nodeName: cfg.NodeID,
		client:   client,
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

	return instanceID, nil
}

func providerInstanceID(node *corev1.Node) (string, error) {
	if node == nil {
		return "", fmt.Errorf("node is nil")
	}

	providerID := strings.TrimSpace(node.Spec.ProviderID)
	if providerID == "" {
		return "", fmt.Errorf("node %q does not have spec.providerID set", node.Name)
	}

	match := providerIDUUIDPattern.FindStringSubmatch(providerID)
	if len(match) != 2 {
		return "", fmt.Errorf("node %q has unsupported providerID format %q", node.Name, providerID)
	}

	return strings.ToLower(match[1]), nil
}
