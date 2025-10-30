package k8s

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestClient_CreateConfigMap tests ConfigMap creation
func TestClient_CreateConfigMap(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	data := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}
	labels := map[string]string{
		"app": "test",
	}

	cm, err := client.CreateConfigMap("test-ns", "test-cm", data, labels)

	require.NoError(t, err)
	assert.NotNil(t, cm)
	assert.Equal(t, "test-cm", cm.Name)
	assert.Equal(t, "test-ns", cm.Namespace)
	assert.Equal(t, data, cm.Data)
	assert.Equal(t, labels, cm.Labels)

	// Verify it was created in the fake clientset
	createdCM, err := fakeClient.CoreV1().ConfigMaps("test-ns").Get(
		context.Background(), "test-cm", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, data, createdCM.Data)
}

// TestClient_GetConfigMap tests ConfigMap retrieval
func TestClient_GetConfigMap(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	// Create a ConfigMap first
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "test-ns",
			Labels:    map[string]string{"app": "test"},
		},
		Data: map[string]string{"key": "value"},
	}
	_, err := fakeClient.CoreV1().ConfigMaps("test-ns").Create(
		context.Background(), cm, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Get the ConfigMap
	retrievedCM, err := client.GetConfigMap("test-ns", "test-cm")

	require.NoError(t, err)
	assert.NotNil(t, retrievedCM)
	assert.Equal(t, "test-cm", retrievedCM.Name)
	assert.Equal(t, map[string]string{"key": "value"}, retrievedCM.Data)
}

// TestClient_GetConfigMap_NotFound tests error when ConfigMap doesn't exist
func TestClient_GetConfigMap_NotFound(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	_, err := client.GetConfigMap("test-ns", "nonexistent-cm")

	assert.Error(t, err)
}

// TestClient_UpdateConfigMap tests ConfigMap update
func TestClient_UpdateConfigMap(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	// Create a ConfigMap first
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "test-ns",
		},
		Data: map[string]string{"key": "oldvalue"},
	}
	_, err := fakeClient.CoreV1().ConfigMaps("test-ns").Create(
		context.Background(), cm, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Update the ConfigMap
	newData := map[string]string{"key": "newvalue", "key2": "value2"}
	updatedCM, err := client.UpdateConfigMap("test-ns", "test-cm", newData)

	require.NoError(t, err)
	assert.NotNil(t, updatedCM)
	assert.Equal(t, newData, updatedCM.Data)

	// Verify the update in fake clientset
	retrievedCM, err := fakeClient.CoreV1().ConfigMaps("test-ns").Get(
		context.Background(), "test-cm", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, newData, retrievedCM.Data)
}

// TestClient_UpdateConfigMap_NotFound tests error when ConfigMap doesn't exist
func TestClient_UpdateConfigMap_NotFound(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	_, err := client.UpdateConfigMap("test-ns", "nonexistent-cm", map[string]string{"key": "value"})

	assert.Error(t, err)
}

// TestClient_DeleteConfigMap tests ConfigMap deletion
func TestClient_DeleteConfigMap(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	// Create a ConfigMap first
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "test-ns",
		},
		Data: map[string]string{"key": "value"},
	}
	_, err := fakeClient.CoreV1().ConfigMaps("test-ns").Create(
		context.Background(), cm, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Delete the ConfigMap
	err = client.DeleteConfigMap("test-ns", "test-cm")

	require.NoError(t, err)

	// Verify it was deleted
	_, err = fakeClient.CoreV1().ConfigMaps("test-ns").Get(
		context.Background(), "test-cm", metav1.GetOptions{},
	)
	assert.Error(t, err)
}

// TestClient_EnsureConfigMap_Create tests EnsureConfigMap when ConfigMap doesn't exist
func TestClient_EnsureConfigMap_Create(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	data := map[string]string{"key": "value"}
	labels := map[string]string{"app": "test"}

	cm, err := client.EnsureConfigMap("test-ns", "test-cm", data, labels)

	require.NoError(t, err)
	assert.NotNil(t, cm)
	assert.Equal(t, "test-cm", cm.Name)
	assert.Equal(t, data, cm.Data)
	assert.Equal(t, labels, cm.Labels)

	// Verify it was created
	createdCM, err := fakeClient.CoreV1().ConfigMaps("test-ns").Get(
		context.Background(), "test-cm", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, data, createdCM.Data)
}

// TestClient_EnsureConfigMap_Update tests EnsureConfigMap when ConfigMap exists
func TestClient_EnsureConfigMap_Update(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	// Create existing ConfigMap
	existingCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "test-ns",
		},
		Data: map[string]string{"key": "oldvalue"},
	}
	_, err := fakeClient.CoreV1().ConfigMaps("test-ns").Create(
		context.Background(), existingCM, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Ensure with new data
	newData := map[string]string{"key": "newvalue", "key2": "value2"}
	labels := map[string]string{"app": "updated"}

	cm, err := client.EnsureConfigMap("test-ns", "test-cm", newData, labels)

	require.NoError(t, err)
	assert.NotNil(t, cm)
	assert.Equal(t, newData, cm.Data)

	// Verify it was updated
	updatedCM, err := fakeClient.CoreV1().ConfigMaps("test-ns").Get(
		context.Background(), "test-cm", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, newData, updatedCM.Data)
}

// TestClient_EnsureConfigMap_NoChange tests EnsureConfigMap when data matches
func TestClient_EnsureConfigMap_NoChange(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	data := map[string]string{"key": "value"}

	// Create existing ConfigMap
	existingCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "test-ns",
		},
		Data: data,
	}
	_, err := fakeClient.CoreV1().ConfigMaps("test-ns").Create(
		context.Background(), existingCM, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Ensure with same data
	cm, err := client.EnsureConfigMap("test-ns", "test-cm", data, nil)

	require.NoError(t, err)
	assert.NotNil(t, cm)
	assert.Equal(t, data, cm.Data)
}
