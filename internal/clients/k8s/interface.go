package k8s

import "k8s.io/client-go/kubernetes"

// Interface defines the contract for Kubernetes client operations
// This interface allows for easy mocking in tests
type Interface interface {
	// Clientset returns the underlying Kubernetes clientset
	// Useful for direct API access when needed
	Clientset() kubernetes.Interface

	// Port forwarding operations
	PortForwardService(namespace, serviceName string, localPort, remotePort int) (stopChan chan struct{}, readyChan chan struct{}, err error)

	// Deployment scaling operations
	ScaleDownDeployments(namespace, labelSelector string) ([]AppsScale, error)
	ScaleUpDeploymentsFromAnnotations(namespace, labelSelector string) ([]AppsScale, error)

	// StatefulSet scaling operations
	ScaleDownStatefulSets(namespace, labelSelector string) ([]AppsScale, error)
	ScaleUpStatefulSetsFromAnnotations(namespace, labelSelector string) ([]AppsScale, error)
}

// Ensure *Client implements Interface
var _ Interface = (*Client)(nil)
