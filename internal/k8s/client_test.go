package k8s

import (
	"context"
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
		expectedScales []DeploymentScale
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
			expectedScales: []DeploymentScale{
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
			expectedScales: []DeploymentScale{
				{Name: "deploy1", Replicas: 0},
			},
			expectError: false,
		},
		{
			name:           "no deployments matching selector",
			namespace:      "test-ns",
			labelSelector:  "app=nonexistent",
			deployments:    []appsv1.Deployment{},
			expectedScales: []DeploymentScale{},
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
			expectedScales: []DeploymentScale{
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
				}
			}
		})
	}
}

func TestClient_ScaleUpDeployments(t *testing.T) {
	tests := []struct {
		name            string
		namespace       string
		initialReplicas int32
		scaleToReplicas int32
		deploymentName  string
		expectError     bool
	}{
		{
			name:            "scale up from zero to three",
			namespace:       "test-ns",
			initialReplicas: 0,
			scaleToReplicas: 3,
			deploymentName:  "test-deploy",
			expectError:     false,
		},
		{
			name:            "scale up from two to five",
			namespace:       "test-ns",
			initialReplicas: 2,
			scaleToReplicas: 5,
			deploymentName:  "test-deploy",
			expectError:     false,
		},
		{
			name:            "restore to zero replicas",
			namespace:       "test-ns",
			initialReplicas: 3,
			scaleToReplicas: 0,
			deploymentName:  "test-deploy",
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake clientset with deployment at initial scale
			fakeClient := fake.NewSimpleClientset()
			deploy := createDeployment(tt.deploymentName, tt.namespace, map[string]string{"app": "test"}, tt.initialReplicas)
			_, err := fakeClient.AppsV1().Deployments(tt.namespace).Create(
				context.Background(), &deploy, metav1.CreateOptions{},
			)
			require.NoError(t, err)

			// Create our client wrapper
			client := &Client{
				clientset: fakeClient,
			}

			// Execute scale up
			scales := []DeploymentScale{
				{Name: tt.deploymentName, Replicas: tt.scaleToReplicas},
			}
			err = client.ScaleUpDeployments(tt.namespace, scales)

			// Assertions
			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Verify the deployment was scaled to expected replicas
			updatedDeploy, err := fakeClient.AppsV1().Deployments(tt.namespace).Get(
				context.Background(), tt.deploymentName, metav1.GetOptions{},
			)
			require.NoError(t, err)
			assert.Equal(t, tt.scaleToReplicas, *updatedDeploy.Spec.Replicas)
		})
	}
}

func TestClient_ScaleUpDeployments_NonExistent(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{
		clientset: fakeClient,
	}

	scales := []DeploymentScale{
		{Name: "nonexistent-deploy", Replicas: 3},
	}
	err := client.ScaleUpDeployments("test-ns", scales)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get deployment")
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
