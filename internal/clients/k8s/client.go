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
	"path"
	"path/filepath"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/client-go/util/retry"
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

// PortForwardService creates a port-forward to a Kubernetes service.
// It uses OS dynamic port allocation (port 0) and returns the actual allocated local port.
func (c *Client) PortForwardService(namespace, serviceName string, remotePort int) (chan struct{}, int, error) {
	ctx := context.Background()

	// Get service to find pods
	svc, err := c.clientset.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get service: %w", err)
	}

	// Find pod matching service selector
	podList, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(&metav1.LabelSelector{
			MatchLabels: svc.Spec.Selector,
		}),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list pods: %w", err)
	}

	if len(podList.Items) == 0 {
		return nil, 0, fmt.Errorf("no pods found for service %s", serviceName)
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
		return nil, 0, fmt.Errorf("no running pods found for service %s", serviceName)
	}
	// Setup port-forward
	return c.PortForwardPod(namespace, targetPod.Name, remotePort)
}

// portForwardReadyTimeout is the maximum time to wait for a port-forward to become ready.
const portForwardReadyTimeout = 60 * time.Second

// PortForwardPod creates a port-forward to a specific pod using OS dynamic port allocation.
// It waits for the port-forward to be ready and returns the actual allocated local port.
func (c *Client) PortForwardPod(namespace, podName string, remotePort int) (chan struct{}, int, error) {
	reqPath := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName)
	hostIP := c.restConfig.Host
	pfURL, err := url.Parse(hostIP)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse host: %w", err)
	}
	// Preserve any existing path prefix (e.g. from Rancher/OpenShift proxy URLs)
	pfURL.Path = path.Join(pfURL.Path, reqPath)

	transport, upgrader, err := spdy.RoundTripperFor(c.restConfig)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create round tripper: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, pfURL)

	stopChan := make(chan struct{}, 1)
	readyChan := make(chan struct{})

	// Use port 0 so the OS picks a free local port
	ports := []string{fmt.Sprintf("0:%d", remotePort)}

	// Use discard writers if debug is disabled to suppress port-forward output
	outWriter := io.Discard
	errWriter := io.Discard
	if c.debug {
		outWriter = os.Stdout
		errWriter = os.Stderr
	}

	fw, err := portforward.New(dialer, ports, stopChan, readyChan, outWriter, errWriter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create port forwarder: %w", err)
	}

	errChan := make(chan error, 1)
	go func() {
		errChan <- fw.ForwardPorts()
	}()

	// Wait for port-forward to be ready, fail, or timeout
	select {
	case <-readyChan:
		// Port-forward is ready
	case err := <-errChan:
		if err != nil {
			return nil, 0, fmt.Errorf("port forward failed: %w", err)
		}
		return nil, 0, fmt.Errorf("port forward closed unexpectedly")
	case <-time.After(portForwardReadyTimeout):
		close(stopChan)
		return nil, 0, fmt.Errorf("timed out waiting for port-forward to become ready after %s", portForwardReadyTimeout)
	}

	// Drain errChan in background to avoid silent failures if the port-forward
	// dies after becoming ready (e.g., pod eviction, network partition).
	go func() {
		if err := <-errChan; err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "port-forward error: %v\n", err)
		}
	}()

	// Get the actual allocated local port
	forwardedPorts, err := fw.GetPorts()
	if err != nil {
		close(stopChan)
		return nil, 0, fmt.Errorf("failed to get forwarded ports: %w", err)
	}
	if len(forwardedPorts) == 0 {
		close(stopChan)
		return nil, 0, fmt.Errorf("no ports were forwarded")
	}

	actualLocalPort := int(forwardedPorts[0].Local)
	return stopChan, actualLocalPort, nil
}

const (
	// PreRestoreReplicasAnnotation is the annotation key used to store original replica counts
	PreRestoreReplicasAnnotation = "stackstate.com/pre-restore-replicas"

	// RestoreInProgressAnnotation is the annotation key used to track which datastore restore is in progress
	RestoreInProgressAnnotation = "stackstate.com/restore-in-progress"

	// RestoreStartedAtAnnotation is the annotation key used to track when the restore started
	RestoreStartedAtAnnotation = "stackstate.com/restore-started-at"
)

// AppsScale holds the name and original replica count of a scalable resource
type AppsScale struct {
	Name     string
	Replicas int32
}

// RestoreLockInfo holds information about an active restore lock on a resource
type RestoreLockInfo struct {
	ResourceKind string // "Deployment" or "StatefulSet"
	ResourceName string
	Datastore    string
	StartedAt    string
}

// DeploymentUpdateFunc is a function that modifies a deployment.
// It receives a fresh copy of the deployment and should apply the desired changes.
type DeploymentUpdateFunc func(dep *appsv1.Deployment) error

// StatefulSetUpdateFunc is a function that modifies a statefulset.
// It receives a fresh copy of the statefulset and should apply the desired changes.
type StatefulSetUpdateFunc func(sts *appsv1.StatefulSet) error

// updateDeploymentWithRetry fetches a fresh copy of the deployment and applies the update function,
// retrying on conflict errors (when resource version has changed).
func updateDeploymentWithRetry(ctx context.Context, client kubernetes.Interface, namespace, name string, updateFn DeploymentUpdateFunc) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get fresh copy
		dep, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		// Apply changes
		err = updateFn(dep)
		if err != nil {
			return err
		}
		// Update
		_, err = client.AppsV1().Deployments(namespace).Update(ctx, dep, metav1.UpdateOptions{})
		return err
	})
}

// updateStatefulSetWithRetry fetches a fresh copy of the statefulset and applies the update function,
// retrying on conflict errors (when resource version has changed).
func updateStatefulSetWithRetry(ctx context.Context, client kubernetes.Interface, namespace, name string, updateFn StatefulSetUpdateFunc) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get fresh copy
		sts, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		// Apply changes
		err = updateFn(sts)
		if err != nil {
			return err
		}
		// Update
		_, err = client.AppsV1().StatefulSets(namespace).Update(ctx, sts, metav1.UpdateOptions{})
		return err
	})
}

// scaleDownDeployment scales a single deployment to 0 replicas with retry on conflict.
// Returns the original replica count.
//
//nolint:dupl // Deployment and StatefulSet are different K8s types requiring separate implementations
func scaleDownDeployment(ctx context.Context, client kubernetes.Interface, namespace, name string) (int32, error) {
	var originalReplicas int32

	err := updateDeploymentWithRetry(ctx, client, namespace, name, func(dep *appsv1.Deployment) error {
		if dep.Spec.Replicas != nil {
			originalReplicas = *dep.Spec.Replicas
		}
		if dep.Annotations == nil {
			dep.Annotations = make(map[string]string)
		}
		dep.Annotations[PreRestoreReplicasAnnotation] = fmt.Sprintf("%d", originalReplicas)
		zero := int32(0)
		dep.Spec.Replicas = &zero
		return nil
	})

	return originalReplicas, err
}

// scaleDownStatefulSet scales a single statefulset to 0 replicas with retry on conflict.
// Returns the original replica count.
//
//nolint:dupl // Deployment and StatefulSet are different K8s types requiring separate implementations
func scaleDownStatefulSet(ctx context.Context, client kubernetes.Interface, namespace, name string) (int32, error) {
	var originalReplicas int32

	err := updateStatefulSetWithRetry(ctx, client, namespace, name, func(sts *appsv1.StatefulSet) error {
		if sts.Spec.Replicas != nil {
			originalReplicas = *sts.Spec.Replicas
		}
		if sts.Annotations == nil {
			sts.Annotations = make(map[string]string)
		}
		sts.Annotations[PreRestoreReplicasAnnotation] = fmt.Sprintf("%d", originalReplicas)
		zero := int32(0)
		sts.Spec.Replicas = &zero
		return nil
	})

	return originalReplicas, err
}

// scaleUpDeploymentFromAnnotation scales a deployment back to its original replica count with retry on conflict.
// Returns (replica count, found annotation, error). If no annotation was found, returns (0, false, nil).
//
//nolint:dupl // Deployment and StatefulSet are different K8s types requiring separate implementations
func scaleUpDeploymentFromAnnotation(ctx context.Context, client kubernetes.Interface, namespace, name string) (int32, bool, error) {
	var scaledTo int32
	found := true

	err := updateDeploymentWithRetry(ctx, client, namespace, name, func(dep *appsv1.Deployment) error {
		if dep.Annotations == nil {
			found = false
			return nil
		}

		replicasStr, exists := dep.Annotations[PreRestoreReplicasAnnotation]
		if !exists {
			found = false
			return nil
		}

		var originalReplicas int32
		if _, err := fmt.Sscanf(replicasStr, "%d", &originalReplicas); err != nil {
			return fmt.Errorf("failed to parse replicas annotation: %w", err)
		}

		delete(dep.Annotations, PreRestoreReplicasAnnotation)
		dep.Spec.Replicas = &originalReplicas
		scaledTo = originalReplicas

		return nil
	})

	return scaledTo, found, err
}

// scaleUpStatefulSetFromAnnotation scales a statefulset back to its original replica count with retry on conflict.
// Returns (replica count, found annotation, error). If no annotation was found, returns (0, false, nil).
//
//nolint:dupl // Deployment and StatefulSet are different K8s types requiring separate implementations
func scaleUpStatefulSetFromAnnotation(ctx context.Context, client kubernetes.Interface, namespace, name string) (int32, bool, error) {
	var scaledTo int32
	found := true

	err := updateStatefulSetWithRetry(ctx, client, namespace, name, func(sts *appsv1.StatefulSet) error {
		if sts.Annotations == nil {
			found = false
			return nil
		}

		replicasStr, exists := sts.Annotations[PreRestoreReplicasAnnotation]
		if !exists {
			found = false
			return nil
		}

		var originalReplicas int32
		if _, err := fmt.Sscanf(replicasStr, "%d", &originalReplicas); err != nil {
			return fmt.Errorf("failed to parse replicas annotation: %w", err)
		}

		delete(sts.Annotations, PreRestoreReplicasAnnotation)
		sts.Spec.Replicas = &originalReplicas
		scaledTo = originalReplicas

		return nil
	})

	return scaledTo, found, err
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

	var scaledResources []AppsScale
	for _, dep := range deployments.Items {
		originalReplicas, err := scaleDownDeployment(ctx, c.clientset, namespace, dep.Name)
		if err != nil {
			return scaledResources, fmt.Errorf("failed to scale down deployment %s: %w", dep.Name, err)
		}
		scaledResources = append(scaledResources, AppsScale{
			Name:     dep.Name,
			Replicas: originalReplicas,
		})
	}

	return scaledResources, nil
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

	var scaledResources []AppsScale
	for _, dep := range deployments.Items {
		// Check if this deployment has the annotation (to avoid unnecessary API calls)
		if dep.Annotations == nil || dep.Annotations[PreRestoreReplicasAnnotation] == "" {
			continue
		}

		scaledTo, found, err := scaleUpDeploymentFromAnnotation(ctx, c.clientset, namespace, dep.Name)
		if err != nil {
			return scaledResources, fmt.Errorf("failed to scale up deployment %s: %w", dep.Name, err)
		}
		if found {
			scaledResources = append(scaledResources, AppsScale{
				Name:     dep.Name,
				Replicas: scaledTo,
			})
		}
	}

	return scaledResources, nil
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

	var scaledResources []AppsScale
	for _, sts := range statefulSets.Items {
		originalReplicas, err := scaleDownStatefulSet(ctx, c.clientset, namespace, sts.Name)
		if err != nil {
			return scaledResources, fmt.Errorf("failed to scale down statefulset %s: %w", sts.Name, err)
		}
		scaledResources = append(scaledResources, AppsScale{
			Name:     sts.Name,
			Replicas: originalReplicas,
		})
	}

	return scaledResources, nil
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

	var scaledResources []AppsScale
	for _, sts := range statefulSets.Items {
		// Check if this statefulset has the annotation (to avoid unnecessary API calls)
		if sts.Annotations == nil || sts.Annotations[PreRestoreReplicasAnnotation] == "" {
			continue
		}

		scaledTo, found, err := scaleUpStatefulSetFromAnnotation(ctx, c.clientset, namespace, sts.Name)
		if err != nil {
			return scaledResources, fmt.Errorf("failed to scale up statefulset %s: %w", sts.Name, err)
		}
		if found {
			scaledResources = append(scaledResources, AppsScale{
				Name:     sts.Name,
				Replicas: scaledTo,
			})
		}
	}

	return scaledResources, nil
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

// GetRestoreLocks returns all active restore locks on Deployments and StatefulSets
// matching the given label selector
func (c *Client) GetRestoreLocks(namespace, labelSelector string) ([]RestoreLockInfo, error) {
	ctx := context.Background()
	var locks []RestoreLockInfo

	// Check Deployments
	deployments, err := c.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	for _, dep := range deployments.Items {
		if datastore, ok := dep.Annotations[RestoreInProgressAnnotation]; ok {
			locks = append(locks, RestoreLockInfo{
				ResourceKind: "Deployment",
				ResourceName: dep.Name,
				Datastore:    datastore,
				StartedAt:    dep.Annotations[RestoreStartedAtAnnotation],
			})
		}
	}

	// Check StatefulSets
	statefulSets, err := c.clientset.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list statefulsets: %w", err)
	}

	for _, sts := range statefulSets.Items {
		if datastore, ok := sts.Annotations[RestoreInProgressAnnotation]; ok {
			locks = append(locks, RestoreLockInfo{
				ResourceKind: "StatefulSet",
				ResourceName: sts.Name,
				Datastore:    datastore,
				StartedAt:    sts.Annotations[RestoreStartedAtAnnotation],
			})
		}
	}

	return locks, nil
}

// SetRestoreLock sets the restore lock annotations on Deployments and StatefulSets
// matching the given label selector
func (c *Client) SetRestoreLock(namespace, labelSelector, datastore, startedAt string) error {
	ctx := context.Background()

	// Update Deployments
	deployments, err := c.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
	}

	for _, dep := range deployments.Items {
		err := updateDeploymentWithRetry(ctx, c.clientset, namespace, dep.Name, func(d *appsv1.Deployment) error {
			if d.Annotations == nil {
				d.Annotations = make(map[string]string)
			}
			d.Annotations[RestoreInProgressAnnotation] = datastore
			d.Annotations[RestoreStartedAtAnnotation] = startedAt
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to set restore lock on deployment %s: %w", dep.Name, err)
		}
	}

	// Update StatefulSets
	statefulSets, err := c.clientset.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list statefulsets: %w", err)
	}

	for _, sts := range statefulSets.Items {
		err := updateStatefulSetWithRetry(ctx, c.clientset, namespace, sts.Name, func(s *appsv1.StatefulSet) error {
			if s.Annotations == nil {
				s.Annotations = make(map[string]string)
			}
			s.Annotations[RestoreInProgressAnnotation] = datastore
			s.Annotations[RestoreStartedAtAnnotation] = startedAt
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to set restore lock on statefulset %s: %w", sts.Name, err)
		}
	}

	return nil
}

// hasRestoreLockAnnotations checks if the given annotations map contains restore lock annotations
func hasRestoreLockAnnotations(annotations map[string]string) bool {
	if annotations == nil {
		return false
	}
	_, hasLock := annotations[RestoreInProgressAnnotation]
	_, hasStartedAt := annotations[RestoreStartedAtAnnotation]
	return hasLock || hasStartedAt
}

// removeRestoreLockAnnotations removes restore lock annotations from the given map
func removeRestoreLockAnnotations(annotations map[string]string) {
	delete(annotations, RestoreInProgressAnnotation)
	delete(annotations, RestoreStartedAtAnnotation)
}

// ClearRestoreLock removes the restore lock annotations from Deployments and StatefulSets
// matching the given label selector
func (c *Client) ClearRestoreLock(namespace, labelSelector string) error {
	ctx := context.Background()

	// Update Deployments
	deployments, err := c.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
	}

	for _, dep := range deployments.Items {
		if !hasRestoreLockAnnotations(dep.Annotations) {
			continue
		}

		err := updateDeploymentWithRetry(ctx, c.clientset, namespace, dep.Name, func(d *appsv1.Deployment) error {
			if d.Annotations != nil {
				removeRestoreLockAnnotations(d.Annotations)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to clear restore lock on deployment %s: %w", dep.Name, err)
		}
	}

	// Update StatefulSets
	statefulSets, err := c.clientset.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list statefulsets: %w", err)
	}

	for _, sts := range statefulSets.Items {
		if !hasRestoreLockAnnotations(sts.Annotations) {
			continue
		}

		err := updateStatefulSetWithRetry(ctx, c.clientset, namespace, sts.Name, func(s *appsv1.StatefulSet) error {
			if s.Annotations != nil {
				removeRestoreLockAnnotations(s.Annotations)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to clear restore lock on statefulset %s: %w", sts.Name, err)
		}
	}

	return nil
}
