package k8s

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestClient_ScaleDownDeployments(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		labelSelector  string
		deployments    []appsv1.Deployment
		expectedScales []AppsScale
		expectError    bool
	}{
		{
			name:          "scale down multiple deployments",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments: []appsv1.Deployment{
				createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 3),
				createDeployment("deploy2", "test-ns", map[string]string{"app": "test"}, 5),
			},
			expectedScales: []AppsScale{
				{Name: "deploy1", Replicas: 3},
				{Name: "deploy2", Replicas: 5},
			},
			expectError: false,
		},
		{
			name:          "scale down deployment with zero replicas",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments: []appsv1.Deployment{
				createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 0),
			},
			expectedScales: []AppsScale{
				{Name: "deploy1", Replicas: 0},
			},
			expectError: false,
		},
		{
			name:           "no deployments matching selector",
			namespace:      "test-ns",
			labelSelector:  "app=nonexistent",
			deployments:    []appsv1.Deployment{},
			expectedScales: []AppsScale{},
			expectError:    false,
		},
		{
			name:          "deployments with different labels not selected",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments: []appsv1.Deployment{
				createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 3),
				createDeployment("deploy2", "test-ns", map[string]string{"app": "other"}, 2),
			},
			expectedScales: []AppsScale{
				{Name: "deploy1", Replicas: 3},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake clientset with test deployments
			fakeClient := fake.NewSimpleClientset()
			for _, deploy := range tt.deployments {
				_, err := fakeClient.AppsV1().Deployments(tt.namespace).Create(
					context.Background(), &deploy, metav1.CreateOptions{},
				)
				require.NoError(t, err)
			}

			// Create our client wrapper
			client := &Client{
				clientset: fakeClient,
			}

			// Execute scale down
			scales, err := client.ScaleDownDeployments(tt.namespace, tt.labelSelector)

			// Assertions
			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, len(tt.expectedScales), len(scales))

			// Verify each scaled deployment
			for i, expectedScale := range tt.expectedScales {
				assert.Equal(t, expectedScale.Name, scales[i].Name)
				assert.Equal(t, expectedScale.Replicas, scales[i].Replicas)

				// Verify the deployment was actually scaled to 0
				deploy, err := fakeClient.AppsV1().Deployments(tt.namespace).Get(
					context.Background(), expectedScale.Name, metav1.GetOptions{},
				)
				require.NoError(t, err)
				if expectedScale.Replicas > 0 {
					assert.Equal(t, int32(0), *deploy.Spec.Replicas, "deployment should be scaled to 0")
					// Verify annotation was added with original replica count
					assert.Equal(t, fmt.Sprintf("%d", expectedScale.Replicas), deploy.Annotations[PreRestoreReplicasAnnotation], "annotation should be added with original replica count")
				}
			}
		})
	}
}

//nolint:funlen // Table-driven test with comprehensive test cases
func TestClient_ScaleUpDeploymentsFromAnnotations(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		labelSelector  string
		deployments    []appsv1.Deployment
		expectedScales []AppsScale
		expectError    bool
		errorContains  string
	}{
		{
			name:          "scale up multiple deployments from annotations",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments: []appsv1.Deployment{
				func() appsv1.Deployment {
					d := createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 0)
					d.Annotations = map[string]string{PreRestoreReplicasAnnotation: "3"}
					return d
				}(),
				func() appsv1.Deployment {
					d := createDeployment("deploy2", "test-ns", map[string]string{"app": "test"}, 0)
					d.Annotations = map[string]string{PreRestoreReplicasAnnotation: "5"}
					return d
				}(),
			},
			expectedScales: []AppsScale{
				{Name: "deploy1", Replicas: 3},
				{Name: "deploy2", Replicas: 5},
			},
			expectError: false,
		},
		{
			name:          "no deployments with annotations",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments: []appsv1.Deployment{
				createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 0),
			},
			expectedScales: []AppsScale{},
			expectError:    false,
		},
		{
			name:          "mixed deployments with and without annotations",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments: []appsv1.Deployment{
				func() appsv1.Deployment {
					d := createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 0)
					d.Annotations = map[string]string{PreRestoreReplicasAnnotation: "3"}
					return d
				}(),
				createDeployment("deploy2", "test-ns", map[string]string{"app": "test"}, 0),
			},
			expectedScales: []AppsScale{
				{Name: "deploy1", Replicas: 3},
			},
			expectError: false,
		},
		{
			name:          "invalid annotation value",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments: []appsv1.Deployment{
				func() appsv1.Deployment {
					d := createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 0)
					d.Annotations = map[string]string{PreRestoreReplicasAnnotation: "invalid"}
					return d
				}(),
			},
			expectedScales: []AppsScale{},
			expectError:    true,
			errorContains:  "failed to parse replicas annotation",
		},
		{
			name:          "scale to zero replicas",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments: []appsv1.Deployment{
				func() appsv1.Deployment {
					d := createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 0)
					d.Annotations = map[string]string{PreRestoreReplicasAnnotation: "0"}
					return d
				}(),
			},
			expectedScales: []AppsScale{
				{Name: "deploy1", Replicas: 0},
			},
			expectError: false,
		},
		{
			name:           "no deployments matching selector",
			namespace:      "test-ns",
			labelSelector:  "app=test",
			deployments:    []appsv1.Deployment{},
			expectedScales: []AppsScale{},
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake clientset with test deployments
			fakeClient := fake.NewSimpleClientset()
			for _, deploy := range tt.deployments {
				_, err := fakeClient.AppsV1().Deployments(tt.namespace).Create(
					context.Background(), &deploy, metav1.CreateOptions{},
				)
				require.NoError(t, err)
			}

			// Create our client wrapper
			client := &Client{
				clientset: fakeClient,
			}

			// Execute scale up from annotations
			scales, err := client.ScaleUpDeploymentsFromAnnotations(tt.namespace, tt.labelSelector)

			// Assertions
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, len(tt.expectedScales), len(scales))

			// Verify each scaled deployment
			for i, expectedScale := range tt.expectedScales {
				assert.Equal(t, expectedScale.Name, scales[i].Name)
				assert.Equal(t, expectedScale.Replicas, scales[i].Replicas)

				// Verify the deployment was actually scaled to expected replicas
				deploy, err := fakeClient.AppsV1().Deployments(tt.namespace).Get(
					context.Background(), expectedScale.Name, metav1.GetOptions{},
				)
				require.NoError(t, err)
				assert.Equal(t, expectedScale.Replicas, *deploy.Spec.Replicas, "deployment should be scaled to expected replicas")

				// Verify annotation was removed
				_, exists := deploy.Annotations[PreRestoreReplicasAnnotation]
				assert.False(t, exists, "annotation should be removed after scale up")
			}
		})
	}
}

func TestClient_ScaleDownThenScaleUpFromAnnotations(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()

	// Create deployments with different replica counts
	deploy1 := createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 3)
	deploy2 := createDeployment("deploy2", "test-ns", map[string]string{"app": "test"}, 5)

	_, err := fakeClient.AppsV1().Deployments("test-ns").Create(
		context.Background(), &deploy1, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	_, err = fakeClient.AppsV1().Deployments("test-ns").Create(
		context.Background(), &deploy2, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	client := &Client{
		clientset: fakeClient,
	}

	// Scale down
	scaledDown, err := client.ScaleDownDeployments("test-ns", "app=test")
	require.NoError(t, err)
	assert.Len(t, scaledDown, 2)

	// Verify deployments are scaled to 0
	deploy1After, err := fakeClient.AppsV1().Deployments("test-ns").Get(
		context.Background(), "deploy1", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, int32(0), *deploy1After.Spec.Replicas)

	// Verify annotations were added
	assert.Equal(t, "3", deploy1After.Annotations[PreRestoreReplicasAnnotation])

	deploy2After, err := fakeClient.AppsV1().Deployments("test-ns").Get(
		context.Background(), "deploy2", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, "5", deploy2After.Annotations[PreRestoreReplicasAnnotation])

	// Scale up from annotations
	scaledUp, err := client.ScaleUpDeploymentsFromAnnotations("test-ns", "app=test")
	require.NoError(t, err)
	assert.Len(t, scaledUp, 2)

	// Verify deployments are scaled back to original replicas
	deploy1Final, err := fakeClient.AppsV1().Deployments("test-ns").Get(
		context.Background(), "deploy1", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, int32(3), *deploy1Final.Spec.Replicas)

	// Verify annotations were removed
	_, exists := deploy1Final.Annotations[PreRestoreReplicasAnnotation]
	assert.False(t, exists)

	deploy2Final, err := fakeClient.AppsV1().Deployments("test-ns").Get(
		context.Background(), "deploy2", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, int32(5), *deploy2Final.Spec.Replicas)

	_, exists = deploy2Final.Annotations[PreRestoreReplicasAnnotation]
	assert.False(t, exists)
}

func TestClient_Clientset(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{
		clientset: fakeClient,
	}

	clientset := client.Clientset()
	assert.NotNil(t, clientset)
	assert.Equal(t, fakeClient, clientset)
}

func TestClient_PortForwardService_ServiceNotFound(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{
		clientset: fakeClient,
	}

	_, _, err := client.PortForwardService("test-ns", "nonexistent-svc", 8080, 9200)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get service")
}

func TestClient_PortForwardService_NoPodsFound(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()

	// Create a service without any matching pods
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "test-ns",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "test"},
		},
	}
	_, err := fakeClient.CoreV1().Services("test-ns").Create(
		context.Background(), svc, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	client := &Client{
		clientset: fakeClient,
	}

	_, _, err = client.PortForwardService("test-ns", "test-svc", 8080, 9200)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no pods found for service")
}

func TestClient_PortForwardService_NoRunningPods(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()

	// Create a service
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "test-ns",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "test"},
		},
	}
	_, err := fakeClient.CoreV1().Services("test-ns").Create(
		context.Background(), svc, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Create a pod in Pending state
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-ns",
			Labels:    map[string]string{"app": "test"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}
	_, err = fakeClient.CoreV1().Pods("test-ns").Create(
		context.Background(), pod, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	client := &Client{
		clientset: fakeClient,
	}

	_, _, err = client.PortForwardService("test-ns", "test-svc", 8080, 9200)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no running pods found for service")
}

// Helper function to create a deployment for testing
//
//nolint:unparam // namespace parameter is always "test-ns" in current tests, but kept for flexibility
func createDeployment(name, namespace string, labels map[string]string, replicas int32) appsv1.Deployment {
	return appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
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

// Helper function to create a statefulset for testing
//
//nolint:unparam // namespace parameter is always "test-ns" in current tests, but kept for flexibility
func createStatefulSet(name, namespace string, labels map[string]string, replicas int32) appsv1.StatefulSet {
	return appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
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

//nolint:funlen // Table-driven test with comprehensive test cases
func TestClient_GetRestoreLocks(t *testing.T) {
	tests := []struct {
		name          string
		namespace     string
		labelSelector string
		deployments   []appsv1.Deployment
		statefulSets  []appsv1.StatefulSet
		expectedLocks []RestoreLockInfo
	}{
		{
			name:          "no locks",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments: []appsv1.Deployment{
				createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 3),
			},
			statefulSets:  []appsv1.StatefulSet{},
			expectedLocks: nil,
		},
		{
			name:          "lock on deployment",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments: []appsv1.Deployment{
				func() appsv1.Deployment {
					d := createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 0)
					d.Annotations = map[string]string{
						RestoreInProgressAnnotation: "elasticsearch",
						RestoreStartedAtAnnotation:  "2025-01-01T12:00:00Z",
					}
					return d
				}(),
			},
			statefulSets: []appsv1.StatefulSet{},
			expectedLocks: []RestoreLockInfo{
				{
					ResourceKind: "Deployment",
					ResourceName: "deploy1",
					Datastore:    "elasticsearch",
					StartedAt:    "2025-01-01T12:00:00Z",
				},
			},
		},
		{
			name:          "lock on statefulset",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments:   []appsv1.Deployment{},
			statefulSets: []appsv1.StatefulSet{
				func() appsv1.StatefulSet {
					s := createStatefulSet("sts1", "test-ns", map[string]string{"app": "test"}, 0)
					s.Annotations = map[string]string{
						RestoreInProgressAnnotation: "victoriametrics",
						RestoreStartedAtAnnotation:  "2025-01-01T13:00:00Z",
					}
					return s
				}(),
			},
			expectedLocks: []RestoreLockInfo{
				{
					ResourceKind: "StatefulSet",
					ResourceName: "sts1",
					Datastore:    "victoriametrics",
					StartedAt:    "2025-01-01T13:00:00Z",
				},
			},
		},
		{
			name:          "locks on both deployment and statefulset",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments: []appsv1.Deployment{
				func() appsv1.Deployment {
					d := createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 0)
					d.Annotations = map[string]string{
						RestoreInProgressAnnotation: "stackgraph",
						RestoreStartedAtAnnotation:  "2025-01-01T12:00:00Z",
					}
					return d
				}(),
			},
			statefulSets: []appsv1.StatefulSet{
				func() appsv1.StatefulSet {
					s := createStatefulSet("sts1", "test-ns", map[string]string{"app": "test"}, 0)
					s.Annotations = map[string]string{
						RestoreInProgressAnnotation: "stackgraph",
						RestoreStartedAtAnnotation:  "2025-01-01T12:00:00Z",
					}
					return s
				}(),
			},
			expectedLocks: []RestoreLockInfo{
				{
					ResourceKind: "Deployment",
					ResourceName: "deploy1",
					Datastore:    "stackgraph",
					StartedAt:    "2025-01-01T12:00:00Z",
				},
				{
					ResourceKind: "StatefulSet",
					ResourceName: "sts1",
					Datastore:    "stackgraph",
					StartedAt:    "2025-01-01T12:00:00Z",
				},
			},
		},
		{
			name:          "only matching label selector",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments: []appsv1.Deployment{
				func() appsv1.Deployment {
					d := createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 0)
					d.Annotations = map[string]string{
						RestoreInProgressAnnotation: "elasticsearch",
						RestoreStartedAtAnnotation:  "2025-01-01T12:00:00Z",
					}
					return d
				}(),
				func() appsv1.Deployment {
					d := createDeployment("deploy2", "test-ns", map[string]string{"app": "other"}, 0)
					d.Annotations = map[string]string{
						RestoreInProgressAnnotation: "elasticsearch",
						RestoreStartedAtAnnotation:  "2025-01-01T12:00:00Z",
					}
					return d
				}(),
			},
			statefulSets: []appsv1.StatefulSet{},
			expectedLocks: []RestoreLockInfo{
				{
					ResourceKind: "Deployment",
					ResourceName: "deploy1",
					Datastore:    "elasticsearch",
					StartedAt:    "2025-01-01T12:00:00Z",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset()

			for _, deploy := range tt.deployments {
				_, err := fakeClient.AppsV1().Deployments(tt.namespace).Create(
					context.Background(), &deploy, metav1.CreateOptions{},
				)
				require.NoError(t, err)
			}

			for _, sts := range tt.statefulSets {
				_, err := fakeClient.AppsV1().StatefulSets(tt.namespace).Create(
					context.Background(), &sts, metav1.CreateOptions{},
				)
				require.NoError(t, err)
			}

			client := &Client{clientset: fakeClient}

			locks, err := client.GetRestoreLocks(tt.namespace, tt.labelSelector)
			require.NoError(t, err)

			assert.Equal(t, len(tt.expectedLocks), len(locks))
			for i, expected := range tt.expectedLocks {
				assert.Equal(t, expected.ResourceKind, locks[i].ResourceKind)
				assert.Equal(t, expected.ResourceName, locks[i].ResourceName)
				assert.Equal(t, expected.Datastore, locks[i].Datastore)
				assert.Equal(t, expected.StartedAt, locks[i].StartedAt)
			}
		})
	}
}

func TestClient_SetRestoreLock(t *testing.T) {
	tests := []struct {
		name          string
		namespace     string
		labelSelector string
		datastore     string
		startedAt     string
		deployments   []appsv1.Deployment
		statefulSets  []appsv1.StatefulSet
	}{
		{
			name:          "set lock on deployment",
			namespace:     "test-ns",
			labelSelector: "app=test",
			datastore:     "elasticsearch",
			startedAt:     "2025-01-01T12:00:00Z",
			deployments: []appsv1.Deployment{
				createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 3),
			},
			statefulSets: []appsv1.StatefulSet{},
		},
		{
			name:          "set lock on statefulset",
			namespace:     "test-ns",
			labelSelector: "app=test",
			datastore:     "victoriametrics",
			startedAt:     "2025-01-01T13:00:00Z",
			deployments:   []appsv1.Deployment{},
			statefulSets: []appsv1.StatefulSet{
				createStatefulSet("sts1", "test-ns", map[string]string{"app": "test"}, 3),
			},
		},
		{
			name:          "set lock on multiple resources",
			namespace:     "test-ns",
			labelSelector: "app=test",
			datastore:     "stackgraph",
			startedAt:     "2025-01-01T14:00:00Z",
			deployments: []appsv1.Deployment{
				createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 3),
				createDeployment("deploy2", "test-ns", map[string]string{"app": "test"}, 2),
			},
			statefulSets: []appsv1.StatefulSet{
				createStatefulSet("sts1", "test-ns", map[string]string{"app": "test"}, 1),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset()

			for _, deploy := range tt.deployments {
				_, err := fakeClient.AppsV1().Deployments(tt.namespace).Create(
					context.Background(), &deploy, metav1.CreateOptions{},
				)
				require.NoError(t, err)
			}

			for _, sts := range tt.statefulSets {
				_, err := fakeClient.AppsV1().StatefulSets(tt.namespace).Create(
					context.Background(), &sts, metav1.CreateOptions{},
				)
				require.NoError(t, err)
			}

			client := &Client{clientset: fakeClient}

			err := client.SetRestoreLock(tt.namespace, tt.labelSelector, tt.datastore, tt.startedAt)
			require.NoError(t, err)

			// Verify locks were set on deployments
			for _, deploy := range tt.deployments {
				d, err := fakeClient.AppsV1().Deployments(tt.namespace).Get(
					context.Background(), deploy.Name, metav1.GetOptions{},
				)
				require.NoError(t, err)
				assert.Equal(t, tt.datastore, d.Annotations[RestoreInProgressAnnotation])
				assert.Equal(t, tt.startedAt, d.Annotations[RestoreStartedAtAnnotation])
			}

			// Verify locks were set on statefulsets
			for _, sts := range tt.statefulSets {
				s, err := fakeClient.AppsV1().StatefulSets(tt.namespace).Get(
					context.Background(), sts.Name, metav1.GetOptions{},
				)
				require.NoError(t, err)
				assert.Equal(t, tt.datastore, s.Annotations[RestoreInProgressAnnotation])
				assert.Equal(t, tt.startedAt, s.Annotations[RestoreStartedAtAnnotation])
			}
		})
	}
}

//nolint:funlen // Table-driven test with comprehensive test cases
func TestClient_ClearRestoreLock(t *testing.T) {
	tests := []struct {
		name          string
		namespace     string
		labelSelector string
		deployments   []appsv1.Deployment
		statefulSets  []appsv1.StatefulSet
	}{
		{
			name:          "clear lock from deployment",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments: []appsv1.Deployment{
				func() appsv1.Deployment {
					d := createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 0)
					d.Annotations = map[string]string{
						RestoreInProgressAnnotation: "elasticsearch",
						RestoreStartedAtAnnotation:  "2025-01-01T12:00:00Z",
					}
					return d
				}(),
			},
			statefulSets: []appsv1.StatefulSet{},
		},
		{
			name:          "clear lock from statefulset",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments:   []appsv1.Deployment{},
			statefulSets: []appsv1.StatefulSet{
				func() appsv1.StatefulSet {
					s := createStatefulSet("sts1", "test-ns", map[string]string{"app": "test"}, 0)
					s.Annotations = map[string]string{
						RestoreInProgressAnnotation: "victoriametrics",
						RestoreStartedAtAnnotation:  "2025-01-01T13:00:00Z",
					}
					return s
				}(),
			},
		},
		{
			name:          "clear locks from multiple resources",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments: []appsv1.Deployment{
				func() appsv1.Deployment {
					d := createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 0)
					d.Annotations = map[string]string{
						RestoreInProgressAnnotation: "stackgraph",
						RestoreStartedAtAnnotation:  "2025-01-01T12:00:00Z",
					}
					return d
				}(),
			},
			statefulSets: []appsv1.StatefulSet{
				func() appsv1.StatefulSet {
					s := createStatefulSet("sts1", "test-ns", map[string]string{"app": "test"}, 0)
					s.Annotations = map[string]string{
						RestoreInProgressAnnotation: "stackgraph",
						RestoreStartedAtAnnotation:  "2025-01-01T12:00:00Z",
					}
					return s
				}(),
			},
		},
		{
			name:          "clear lock preserves other annotations",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments: []appsv1.Deployment{
				func() appsv1.Deployment {
					d := createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 0)
					d.Annotations = map[string]string{
						RestoreInProgressAnnotation:  "elasticsearch",
						RestoreStartedAtAnnotation:   "2025-01-01T12:00:00Z",
						PreRestoreReplicasAnnotation: "3",
						"custom-annotation":          "custom-value",
					}
					return d
				}(),
			},
			statefulSets: []appsv1.StatefulSet{},
		},
		{
			name:          "no-op when no locks present",
			namespace:     "test-ns",
			labelSelector: "app=test",
			deployments: []appsv1.Deployment{
				createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 3),
			},
			statefulSets: []appsv1.StatefulSet{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset()

			for _, deploy := range tt.deployments {
				_, err := fakeClient.AppsV1().Deployments(tt.namespace).Create(
					context.Background(), &deploy, metav1.CreateOptions{},
				)
				require.NoError(t, err)
			}

			for _, sts := range tt.statefulSets {
				_, err := fakeClient.AppsV1().StatefulSets(tt.namespace).Create(
					context.Background(), &sts, metav1.CreateOptions{},
				)
				require.NoError(t, err)
			}

			client := &Client{clientset: fakeClient}

			err := client.ClearRestoreLock(tt.namespace, tt.labelSelector)
			require.NoError(t, err)

			// Verify locks were cleared from deployments
			for _, deploy := range tt.deployments {
				d, err := fakeClient.AppsV1().Deployments(tt.namespace).Get(
					context.Background(), deploy.Name, metav1.GetOptions{},
				)
				require.NoError(t, err)

				_, hasLock := d.Annotations[RestoreInProgressAnnotation]
				assert.False(t, hasLock, "restore-in-progress annotation should be removed")

				_, hasStartedAt := d.Annotations[RestoreStartedAtAnnotation]
				assert.False(t, hasStartedAt, "restore-started-at annotation should be removed")

				// Verify other annotations are preserved
				if deploy.Annotations != nil {
					if val, ok := deploy.Annotations["custom-annotation"]; ok {
						assert.Equal(t, val, d.Annotations["custom-annotation"], "custom annotations should be preserved")
					}
					if val, ok := deploy.Annotations[PreRestoreReplicasAnnotation]; ok {
						assert.Equal(t, val, d.Annotations[PreRestoreReplicasAnnotation], "pre-restore-replicas annotation should be preserved")
					}
				}
			}

			// Verify locks were cleared from statefulsets
			for _, sts := range tt.statefulSets {
				s, err := fakeClient.AppsV1().StatefulSets(tt.namespace).Get(
					context.Background(), sts.Name, metav1.GetOptions{},
				)
				require.NoError(t, err)

				_, hasLock := s.Annotations[RestoreInProgressAnnotation]
				assert.False(t, hasLock, "restore-in-progress annotation should be removed")

				_, hasStartedAt := s.Annotations[RestoreStartedAtAnnotation]
				assert.False(t, hasStartedAt, "restore-started-at annotation should be removed")
			}
		})
	}
}

func TestClient_SetAndClearRestoreLock_Integration(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()

	// Create resources
	deploy := createDeployment("deploy1", "test-ns", map[string]string{"app": "test"}, 3)
	sts := createStatefulSet("sts1", "test-ns", map[string]string{"app": "test"}, 2)

	_, err := fakeClient.AppsV1().Deployments("test-ns").Create(
		context.Background(), &deploy, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	_, err = fakeClient.AppsV1().StatefulSets("test-ns").Create(
		context.Background(), &sts, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	client := &Client{clientset: fakeClient}

	// Verify no locks initially
	locks, err := client.GetRestoreLocks("test-ns", "app=test")
	require.NoError(t, err)
	assert.Empty(t, locks)

	// Set locks
	err = client.SetRestoreLock("test-ns", "app=test", "elasticsearch", "2025-01-01T12:00:00Z")
	require.NoError(t, err)

	// Verify locks are set
	locks, err = client.GetRestoreLocks("test-ns", "app=test")
	require.NoError(t, err)
	assert.Len(t, locks, 2)

	// Clear locks
	err = client.ClearRestoreLock("test-ns", "app=test")
	require.NoError(t, err)

	// Verify locks are cleared
	locks, err = client.GetRestoreLocks("test-ns", "app=test")
	require.NoError(t, err)
	assert.Empty(t, locks)
}
