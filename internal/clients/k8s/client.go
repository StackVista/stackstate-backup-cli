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

	appsv1 "k8s.io/api/apps/v1"
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
	pfURL, err := url.Parse(hostIP)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse host: %w", err)
	}
	pfURL.Path = path

	transport, upgrader, err := spdy.RoundTripperFor(c.restConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create round tripper: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, pfURL)

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
				_, _ = fmt.Fprintf(os.Stderr, "Port forward error: %v\n", err)
			}
		}
	}()

	return stopChan, readyChan, nil
}

const (
	// PreRestoreReplicasAnnotation is the annotation key used to store original replica counts
	PreRestoreReplicasAnnotation = "stackstate.com/pre-restore-replicas"
)

// AppsScale holds the name and original replica count of a scalable resource
type AppsScale struct {
	Name     string
	Replicas int32
}

// ScalableResource abstracts operations on resources that can be scaled
type ScalableResource interface {
	GetName() string
	GetReplicas() int32
	SetReplicas(replicas int32)
	GetAnnotations() map[string]string
	SetAnnotations(annotations map[string]string)
	Update(ctx context.Context, client kubernetes.Interface, namespace string) error
}

// deploymentAdapter adapts appsv1.Deployment to ScalableResource interface
type deploymentAdapter struct {
	deployment *appsv1.Deployment
}

func (d *deploymentAdapter) GetName() string {
	return d.deployment.Name
}

func (d *deploymentAdapter) GetReplicas() int32 {
	if d.deployment.Spec.Replicas == nil {
		return 0
	}
	return *d.deployment.Spec.Replicas
}

func (d *deploymentAdapter) SetReplicas(replicas int32) {
	d.deployment.Spec.Replicas = &replicas
}

func (d *deploymentAdapter) GetAnnotations() map[string]string {
	return d.deployment.Annotations
}

func (d *deploymentAdapter) SetAnnotations(annotations map[string]string) {
	d.deployment.Annotations = annotations
}

func (d *deploymentAdapter) Update(ctx context.Context, client kubernetes.Interface, namespace string) error {
	_, err := client.AppsV1().Deployments(namespace).Update(ctx, d.deployment, metav1.UpdateOptions{})
	return err
}

// statefulSetAdapter adapts appsv1.StatefulSet to ScalableResource interface
type statefulSetAdapter struct {
	statefulSet *appsv1.StatefulSet
}

func (s *statefulSetAdapter) GetName() string {
	return s.statefulSet.Name
}

func (s *statefulSetAdapter) GetReplicas() int32 {
	if s.statefulSet.Spec.Replicas == nil {
		return 0
	}
	return *s.statefulSet.Spec.Replicas
}

func (s *statefulSetAdapter) SetReplicas(replicas int32) {
	s.statefulSet.Spec.Replicas = &replicas
}

func (s *statefulSetAdapter) GetAnnotations() map[string]string {
	return s.statefulSet.Annotations
}

func (s *statefulSetAdapter) SetAnnotations(annotations map[string]string) {
	s.statefulSet.Annotations = annotations
}

func (s *statefulSetAdapter) Update(ctx context.Context, client kubernetes.Interface, namespace string) error {
	_, err := client.AppsV1().StatefulSets(namespace).Update(ctx, s.statefulSet, metav1.UpdateOptions{})
	return err
}

// scaleDownResources is a generic function that scales down resources to 0 replicas
func scaleDownResources(ctx context.Context, client kubernetes.Interface, namespace string, resources []ScalableResource) ([]AppsScale, error) {
	if len(resources) == 0 {
		return []AppsScale{}, nil
	}

	var scaledResources []AppsScale

	for _, resource := range resources {
		originalReplicas := resource.GetReplicas()

		// Store original replica count
		scaledResources = append(scaledResources, AppsScale{
			Name:     resource.GetName(),
			Replicas: originalReplicas,
		})

		// Scale to 0 if not already at 0
		if originalReplicas > 0 {
			// Add annotation with original replica count
			annotations := resource.GetAnnotations()
			if annotations == nil {
				annotations = make(map[string]string)
			}
			annotations[PreRestoreReplicasAnnotation] = fmt.Sprintf("%d", originalReplicas)
			resource.SetAnnotations(annotations)
			resource.SetReplicas(0)

			if err := resource.Update(ctx, client, namespace); err != nil {
				return scaledResources, fmt.Errorf("failed to scale down resource %s: %w", resource.GetName(), err)
			}
		}
	}

	return scaledResources, nil
}

// scaleUpResourcesFromAnnotations is a generic function that scales up resources based on annotations
func scaleUpResourcesFromAnnotations(ctx context.Context, client kubernetes.Interface, namespace string, resources []ScalableResource) ([]AppsScale, error) {
	if len(resources) == 0 {
		return []AppsScale{}, nil
	}

	var scaledResources []AppsScale

	for _, resource := range resources {
		annotations := resource.GetAnnotations()
		if annotations == nil {
			continue
		}

		replicasStr, exists := annotations[PreRestoreReplicasAnnotation]
		if !exists {
			continue
		}

		var originalReplicas int32
		if _, err := fmt.Sscanf(replicasStr, "%d", &originalReplicas); err != nil {
			return scaledResources, fmt.Errorf("failed to parse replicas annotation for resource %s: %w", resource.GetName(), err)
		}

		// Scale up to original replica count
		resource.SetReplicas(originalReplicas)

		// Remove the annotation
		delete(annotations, PreRestoreReplicasAnnotation)
		resource.SetAnnotations(annotations)

		if err := resource.Update(ctx, client, namespace); err != nil {
			return scaledResources, fmt.Errorf("failed to scale up resource %s: %w", resource.GetName(), err)
		}

		// Record scaled resource
		scaledResources = append(scaledResources, AppsScale{
			Name:     resource.GetName(),
			Replicas: originalReplicas,
		})
	}

	return scaledResources, nil
}

// ScaleDownDeployments scales down deployments matching a label selector to 0 replicas
// Returns a list of deployment names and their original replica counts
func (c *Client) ScaleDownDeployments(namespace, labelSelector string) ([]AppsScale, error) {
	ctx := context.Background()

	// List deployments matching the label selector
	deployments, err := c.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	// Convert to ScalableResource slice
	resources := make([]ScalableResource, len(deployments.Items))
	for i := range deployments.Items {
		resources[i] = &deploymentAdapter{deployment: &deployments.Items[i]}
	}

	return scaleDownResources(ctx, c.clientset, namespace, resources)
}

// ScaleUpDeploymentsFromAnnotations scales up deployments that have the pre-restore-replicas annotation
// Returns a list of deployments that were scaled up with their replica counts
func (c *Client) ScaleUpDeploymentsFromAnnotations(namespace, labelSelector string) ([]AppsScale, error) {
	ctx := context.Background()

	// List deployments matching the label selector
	deployments, err := c.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	// Convert to ScalableResource slice
	resources := make([]ScalableResource, len(deployments.Items))
	for i := range deployments.Items {
		resources[i] = &deploymentAdapter{deployment: &deployments.Items[i]}
	}

	return scaleUpResourcesFromAnnotations(ctx, c.clientset, namespace, resources)
}

// ScaleDownStatefulSets scales down statefulsets matching a label selector to 0 replicas
// Returns a list of statefulset names and their original replica counts
func (c *Client) ScaleDownStatefulSets(namespace, labelSelector string) ([]AppsScale, error) {
	ctx := context.Background()

	// List statefulsets matching the label selector
	statefulSets, err := c.clientset.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list statefulsets: %w", err)
	}

	// Convert to ScalableResource slice
	resources := make([]ScalableResource, len(statefulSets.Items))
	for i := range statefulSets.Items {
		resources[i] = &statefulSetAdapter{statefulSet: &statefulSets.Items[i]}
	}

	return scaleDownResources(ctx, c.clientset, namespace, resources)
}

// ScaleUpStatefulSetsFromAnnotations scales up statefulsets that have the pre-restore-replicas annotation
// Returns a list of statefulsets that were scaled up with their replica counts
func (c *Client) ScaleUpStatefulSetsFromAnnotations(namespace, labelSelector string) ([]AppsScale, error) {
	ctx := context.Background()

	// List statefulsets matching the label selector
	statefulSets, err := c.clientset.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list statefulsets: %w", err)
	}

	// Convert to ScalableResource slice
	resources := make([]ScalableResource, len(statefulSets.Items))
	for i := range statefulSets.Items {
		resources[i] = &statefulSetAdapter{statefulSet: &statefulSets.Items[i]}
	}

	return scaleUpResourcesFromAnnotations(ctx, c.clientset, namespace, resources)
}

// NewTestClient creates a k8s Client for testing with a fake clientset.
// This function is exported so it can be used in other package tests.
func NewTestClient(clientset kubernetes.Interface) *Client {
	return &Client{
		clientset:  clientset,
		restConfig: nil,
		debug:      false,
	}
}
