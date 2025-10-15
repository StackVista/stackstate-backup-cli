// Package k8s provides Kubernetes client functionality including
// port-forwarding, deployment scaling, and service discovery.
package k8s

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// Client wraps the Kubernetes clientset
type Client struct {
	clientset  kubernetes.Interface
	restConfig *rest.Config
	debug      bool
}

// Clientset returns the underlying Kubernetes clientset
func (c *Client) Clientset() kubernetes.Interface {
	return c.clientset
}

// NewClient creates a new Kubernetes client
func NewClient(kubeconfigPath string, debug bool) (*Client, error) {
	if kubeconfigPath == "" {
		// Use default kubeconfig location
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		kubeconfigPath = filepath.Join(home, ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to build config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return &Client{
		clientset:  clientset,
		restConfig: config,
		debug:      debug,
	}, nil
}

// PortForwardService creates a port-forward to a Kubernetes service
func (c *Client) PortForwardService(namespace, serviceName string, localPort, remotePort int) (chan struct{}, chan struct{}, error) {
	ctx := context.Background()

	// Get service to find pods
	svc, err := c.clientset.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get service: %w", err)
	}

	// Find pod matching service selector
	podList, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(&metav1.LabelSelector{
			MatchLabels: svc.Spec.Selector,
		}),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list pods: %w", err)
	}

	if len(podList.Items) == 0 {
		return nil, nil, fmt.Errorf("no pods found for service %s", serviceName)
	}

	// Find a running pod
	var targetPod *corev1.Pod
	for i := range podList.Items {
		if podList.Items[i].Status.Phase == corev1.PodRunning {
			targetPod = &podList.Items[i]
			break
		}
	}

	if targetPod == nil {
		return nil, nil, fmt.Errorf("no running pods found for service %s", serviceName)
	}
	// Setup port-forward
	return c.PortForwardPod(namespace, targetPod.Name, localPort, remotePort)
}

// PortForwardPod creates a port-forward to a specific pod
func (c *Client) PortForwardPod(namespace, podName string, localPort, remotePort int) (chan struct{}, chan struct{}, error) {
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName)
	hostIP := c.restConfig.Host
	url, err := url.Parse(hostIP)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse host: %w", err)
	}
	url.Path = path

	transport, upgrader, err := spdy.RoundTripperFor(c.restConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create round tripper: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, url)

	stopChan := make(chan struct{}, 1)
	readyChan := make(chan struct{})

	ports := []string{fmt.Sprintf("%d:%d", localPort, remotePort)}

	// Use discard writers if debug is disabled to suppress port-forward output
	outWriter := io.Discard
	errWriter := io.Discard
	if c.debug {
		outWriter = os.Stdout
		errWriter = os.Stderr
	}

	fw, err := portforward.New(dialer, ports, stopChan, readyChan, outWriter, errWriter)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create port forwarder: %w", err)
	}

	go func() {
		if err := fw.ForwardPorts(); err != nil {
			if c.debug {
				fmt.Fprintf(os.Stderr, "Port forward error: %v\n", err)
			}
		}
	}()

	return stopChan, readyChan, nil
}

// DeploymentScale holds the name and original replica count of a deployment
type DeploymentScale struct {
	Name     string
	Replicas int32
}

// ScaleDownDeployments scales down deployments matching a label selector to 0 replicas
// Returns a map of deployment names to their original replica counts
func (c *Client) ScaleDownDeployments(namespace, labelSelector string) ([]DeploymentScale, error) {
	ctx := context.Background()

	// List deployments matching the label selector
	deployments, err := c.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	if len(deployments.Items) == 0 {
		return []DeploymentScale{}, nil
	}

	var scaledDeployments []DeploymentScale

	// Scale down each deployment
	for _, deployment := range deployments.Items {
		originalReplicas := int32(0)
		if deployment.Spec.Replicas != nil {
			originalReplicas = *deployment.Spec.Replicas
		}

		// Store original replica count
		scaledDeployments = append(scaledDeployments, DeploymentScale{
			Name:     deployment.Name,
			Replicas: originalReplicas,
		})

		// Scale to 0 if not already at 0
		if originalReplicas > 0 {
			replicas := int32(0)
			deployment.Spec.Replicas = &replicas

			_, err := c.clientset.AppsV1().Deployments(namespace).Update(ctx, &deployment, metav1.UpdateOptions{})
			if err != nil {
				return scaledDeployments, fmt.Errorf("failed to scale down deployment %s: %w", deployment.Name, err)
			}
		}
	}

	return scaledDeployments, nil
}

// ScaleUpDeployments restores deployments to their original replica counts
func (c *Client) ScaleUpDeployments(namespace string, deploymentScales []DeploymentScale) error {
	ctx := context.Background()

	for _, scale := range deploymentScales {
		deployment, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, scale.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get deployment %s: %w", scale.Name, err)
		}

		deployment.Spec.Replicas = &scale.Replicas

		_, err = c.clientset.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to scale up deployment %s: %w", scale.Name, err)
		}
	}

	return nil
}
