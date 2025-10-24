package scale

import (
	"context"
	"testing"
	"time"

	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestScaleDown_Success tests successful scale down with immediate pod termination
func TestScaleDown_Success(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false) // quiet mode for tests

	// Create test deployments
	deploy1 := createDeployment("deploy1", map[string]string{"app": "test"}, 3)
	deploy2 := createDeployment("deploy2", map[string]string{"app": "test"}, 5)

	_, err := fakeClient.AppsV1().Deployments("test-ns").Create(
		context.Background(), &deploy1, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	_, err = fakeClient.AppsV1().Deployments("test-ns").Create(
		context.Background(), &deploy2, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Create pods that match the label selector
	pod1 := createPod("pod1", map[string]string{"app": "test"}, corev1.PodRunning)
	pod2 := createPod("pod2", map[string]string{"app": "test"}, corev1.PodRunning)

	_, err = fakeClient.CoreV1().Pods("test-ns").Create(
		context.Background(), &pod1, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	_, err = fakeClient.CoreV1().Pods("test-ns").Create(
		context.Background(), &pod2, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Delete pods immediately (simulating fast termination)
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = fakeClient.CoreV1().Pods("test-ns").Delete(
			context.Background(), "pod1", metav1.DeleteOptions{},
		)
		_ = fakeClient.CoreV1().Pods("test-ns").Delete(
			context.Background(), "pod2", metav1.DeleteOptions{},
		)
	}()

	// Execute scale down
	scaledDeployments, err := ScaleDown(client, "test-ns", "app=test", log)

	// Assertions
	require.NoError(t, err)
	assert.Len(t, scaledDeployments, 2)
	assert.Equal(t, "deploy1", scaledDeployments[0].Name)
	assert.Equal(t, int32(3), scaledDeployments[0].Replicas)
	assert.Equal(t, "deploy2", scaledDeployments[1].Name)
	assert.Equal(t, int32(5), scaledDeployments[1].Replicas)

	// Verify deployments were scaled to 0
	deploy1After, err := fakeClient.AppsV1().Deployments("test-ns").Get(
		context.Background(), "deploy1", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, int32(0), *deploy1After.Spec.Replicas)
}

// TestScaleDown_NoDeployments tests scale down when no deployments match the selector
func TestScaleDown_NoDeployments(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false)

	// Create deployment with different labels
	deploy := createDeployment("other-deploy", map[string]string{"app": "other"}, 2)
	_, err := fakeClient.AppsV1().Deployments("test-ns").Create(
		context.Background(), &deploy, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Execute scale down with non-matching selector
	scaledDeployments, err := ScaleDown(client, "test-ns", "app=test", log)

	// Assertions
	require.NoError(t, err)
	assert.Empty(t, scaledDeployments)

	// Verify the other deployment was not scaled
	deployAfter, err := fakeClient.AppsV1().Deployments("test-ns").Get(
		context.Background(), "other-deploy", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, int32(2), *deployAfter.Spec.Replicas)
}

// TestScaleDown_PodsInTerminalState tests that pods in terminal states are ignored
func TestScaleDown_PodsInTerminalState(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false)

	// Create deployment
	deploy := createDeployment("deploy1", map[string]string{"app": "test"}, 3)
	_, err := fakeClient.AppsV1().Deployments("test-ns").Create(
		context.Background(), &deploy, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Create pods in terminal states (should be ignored by waitForPodsToTerminate)
	pod1 := createPod("pod1", map[string]string{"app": "test"}, corev1.PodSucceeded)
	pod2 := createPod("pod2", map[string]string{"app": "test"}, corev1.PodFailed)

	_, err = fakeClient.CoreV1().Pods("test-ns").Create(
		context.Background(), &pod1, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	_, err = fakeClient.CoreV1().Pods("test-ns").Create(
		context.Background(), &pod2, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Delete terminal pods (simulating cleanup)
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = fakeClient.CoreV1().Pods("test-ns").Delete(
			context.Background(), "pod1", metav1.DeleteOptions{},
		)
		_ = fakeClient.CoreV1().Pods("test-ns").Delete(
			context.Background(), "pod2", metav1.DeleteOptions{},
		)
	}()

	// Execute scale down
	scaledDeployments, err := ScaleDown(client, "test-ns", "app=test", log)

	// Assertions - should succeed quickly since pods are in terminal states
	require.NoError(t, err)
	assert.Len(t, scaledDeployments, 1)
}

// TestScaleDown_PodsBeingDeleted tests that pods with DeletionTimestamp are handled correctly
func TestScaleDown_PodsBeingDeleted(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false)

	// Create deployment
	deploy := createDeployment("deploy1", map[string]string{"app": "test"}, 2)
	_, err := fakeClient.AppsV1().Deployments("test-ns").Create(
		context.Background(), &deploy, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Create pod with DeletionTimestamp set (being deleted)
	now := metav1.Now()
	pod := createPod("pod1", map[string]string{"app": "test"}, corev1.PodRunning)
	pod.DeletionTimestamp = &now

	_, err = fakeClient.CoreV1().Pods("test-ns").Create(
		context.Background(), &pod, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Delete pod after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = fakeClient.CoreV1().Pods("test-ns").Delete(
			context.Background(), "pod1", metav1.DeleteOptions{},
		)
	}()

	// Execute scale down
	scaledDeployments, err := ScaleDown(client, "test-ns", "app=test", log)

	// Assertions
	require.NoError(t, err)
	assert.Len(t, scaledDeployments, 1)
}

// TestScaleDown_K8sError tests error handling when K8s API fails
func TestScaleDown_K8sError(t *testing.T) {
	// Note: This test demonstrates the pattern, but fake clientset doesn't easily simulate errors
	// In a real scenario with mocks, we would inject errors
	fakeClient := fake.NewSimpleClientset()
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false)

	// Attempt to scale in non-existent namespace (will return empty list, not error with fake client)
	scaledDeployments, err := ScaleDown(client, "nonexistent-ns", "app=test", log)

	// With fake client, this succeeds with empty results
	require.NoError(t, err)
	assert.Empty(t, scaledDeployments)
}

// TestScaleUp_Success tests successful scale up of deployments
func TestScaleUp_Success(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false)

	// Create deployments at scale 0
	deploy1 := createDeployment("deploy1", map[string]string{"app": "test"}, 0)
	deploy2 := createDeployment("deploy2", map[string]string{"app": "test"}, 0)

	_, err := fakeClient.AppsV1().Deployments("test-ns").Create(
		context.Background(), &deploy1, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	_, err = fakeClient.AppsV1().Deployments("test-ns").Create(
		context.Background(), &deploy2, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Scale up
	deploymentScales := []k8s.DeploymentScale{
		{Name: "deploy1", Replicas: 3},
		{Name: "deploy2", Replicas: 5},
	}

	err = ScaleUp(client, "test-ns", deploymentScales, log)

	// Assertions
	require.NoError(t, err)

	// Verify deployments were scaled up
	deploy1After, err := fakeClient.AppsV1().Deployments("test-ns").Get(
		context.Background(), "deploy1", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, int32(3), *deploy1After.Spec.Replicas)

	deploy2After, err := fakeClient.AppsV1().Deployments("test-ns").Get(
		context.Background(), "deploy2", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, int32(5), *deploy2After.Spec.Replicas)
}

// TestScaleUp_EmptyDeployments tests scale up with empty deployment list
func TestScaleUp_EmptyDeployments(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false)

	// Scale up with empty list
	err := ScaleUp(client, "test-ns", []k8s.DeploymentScale{}, log)

	// Should succeed (no-op)
	require.NoError(t, err)
}

// TestScaleUp_NonexistentDeployment tests error handling when deployment doesn't exist
func TestScaleUp_NonexistentDeployment(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false)

	// Attempt to scale up non-existent deployment
	deploymentScales := []k8s.DeploymentScale{
		{Name: "nonexistent", Replicas: 3},
	}

	err := ScaleUp(client, "test-ns", deploymentScales, log)

	// Should return error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to scale up deployments")
}

// TestScaleDown_IntegrationWithScaleUp tests the full cycle
func TestScaleDown_IntegrationWithScaleUp(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false)

	// Create deployments
	deploy1 := createDeployment("deploy1", map[string]string{"app": "test"}, 3)
	deploy2 := createDeployment("deploy2", map[string]string{"app": "test"}, 5)

	_, err := fakeClient.AppsV1().Deployments("test-ns").Create(
		context.Background(), &deploy1, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	_, err = fakeClient.AppsV1().Deployments("test-ns").Create(
		context.Background(), &deploy2, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Create pods (will be removed to simulate termination)
	pod1 := createPod("pod1", map[string]string{"app": "test"}, corev1.PodRunning)
	_, err = fakeClient.CoreV1().Pods("test-ns").Create(
		context.Background(), &pod1, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Delete pod after delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = fakeClient.CoreV1().Pods("test-ns").Delete(
			context.Background(), "pod1", metav1.DeleteOptions{},
		)
	}()

	// Scale down
	scaledDeployments, err := ScaleDown(client, "test-ns", "app=test", log)
	require.NoError(t, err)
	assert.Len(t, scaledDeployments, 2)

	// Verify scaled to 0
	deploy1After, err := fakeClient.AppsV1().Deployments("test-ns").Get(
		context.Background(), "deploy1", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, int32(0), *deploy1After.Spec.Replicas)

	// Scale back up
	err = ScaleUp(client, "test-ns", scaledDeployments, log)
	require.NoError(t, err)

	// Verify scaled back to original values
	deploy1Final, err := fakeClient.AppsV1().Deployments("test-ns").Get(
		context.Background(), "deploy1", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, int32(3), *deploy1Final.Spec.Replicas)

	deploy2Final, err := fakeClient.AppsV1().Deployments("test-ns").Get(
		context.Background(), "deploy2", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, int32(5), *deploy2Final.Spec.Replicas)
}

// Helper function to create a deployment for testing
func createDeployment(name string, labels map[string]string, replicas int32) appsv1.Deployment {
	return appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-ns",
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test:latest",
						},
					},
				},
			},
		},
	}
}

// Helper function to create a pod for testing
func createPod(name string, labels map[string]string, phase corev1.PodPhase) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-ns",
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "test:latest",
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: phase,
		},
	}
}
