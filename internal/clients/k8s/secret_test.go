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

// TestClient_CreateSecret tests Secret creation
func TestClient_CreateSecret(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	data := map[string][]byte{
		"accesskey": []byte("test-access-key"),
		"secretkey": []byte("test-secret-key"),
	}
	labels := map[string]string{
		"app": "backup",
	}

	secret, err := client.CreateSecret("test-ns", "test-secret", data, labels)

	require.NoError(t, err)
	assert.NotNil(t, secret)
	assert.Equal(t, "test-secret", secret.Name)
	assert.Equal(t, "test-ns", secret.Namespace)
	assert.Equal(t, data, secret.Data)
	assert.Equal(t, labels, secret.Labels)
	assert.Equal(t, corev1.SecretTypeOpaque, secret.Type)

	// Verify it was created in the fake clientset
	createdSecret, err := fakeClient.CoreV1().Secrets("test-ns").Get(
		context.Background(), "test-secret", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, data, createdSecret.Data)
}

// TestClient_GetSecret tests Secret retrieval
func TestClient_GetSecret(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	// Create a Secret first
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-ns",
			Labels:    map[string]string{"app": "test"},
		},
		Data: map[string][]byte{
			"key": []byte("value"),
		},
		Type: corev1.SecretTypeOpaque,
	}
	_, err := fakeClient.CoreV1().Secrets("test-ns").Create(
		context.Background(), secret, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Get the Secret
	retrievedSecret, err := client.GetSecret("test-ns", "test-secret")

	require.NoError(t, err)
	assert.NotNil(t, retrievedSecret)
	assert.Equal(t, "test-secret", retrievedSecret.Name)
	assert.Equal(t, map[string][]byte{"key": []byte("value")}, retrievedSecret.Data)
}

// TestClient_GetSecret_NotFound tests error when Secret doesn't exist
func TestClient_GetSecret_NotFound(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	_, err := client.GetSecret("test-ns", "nonexistent-secret")

	assert.Error(t, err)
}

// TestClient_UpdateSecret tests Secret update
func TestClient_UpdateSecret(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	// Create a Secret first
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			"key": []byte("oldvalue"),
		},
		Type: corev1.SecretTypeOpaque,
	}
	_, err := fakeClient.CoreV1().Secrets("test-ns").Create(
		context.Background(), secret, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Update the Secret
	newData := map[string][]byte{
		"key":  []byte("newvalue"),
		"key2": []byte("value2"),
	}
	updatedSecret, err := client.UpdateSecret("test-ns", "test-secret", newData)

	require.NoError(t, err)
	assert.NotNil(t, updatedSecret)
	assert.Equal(t, newData, updatedSecret.Data)

	// Verify the update in fake clientset
	retrievedSecret, err := fakeClient.CoreV1().Secrets("test-ns").Get(
		context.Background(), "test-secret", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, newData, retrievedSecret.Data)
}

// TestClient_UpdateSecret_NotFound tests error when Secret doesn't exist
func TestClient_UpdateSecret_NotFound(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	_, err := client.UpdateSecret("test-ns", "nonexistent-secret", map[string][]byte{"key": []byte("value")})

	assert.Error(t, err)
}

// TestClient_DeleteSecret tests Secret deletion
func TestClient_DeleteSecret(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	// Create a Secret first
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			"key": []byte("value"),
		},
		Type: corev1.SecretTypeOpaque,
	}
	_, err := fakeClient.CoreV1().Secrets("test-ns").Create(
		context.Background(), secret, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Delete the Secret
	err = client.DeleteSecret("test-ns", "test-secret")

	require.NoError(t, err)

	// Verify it was deleted
	_, err = fakeClient.CoreV1().Secrets("test-ns").Get(
		context.Background(), "test-secret", metav1.GetOptions{},
	)
	assert.Error(t, err)
}

// TestClient_EnsureSecret_Create tests EnsureSecret when Secret doesn't exist
func TestClient_EnsureSecret_Create(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	data := map[string][]byte{
		"accesskey": []byte("access"),
		"secretkey": []byte("secret"),
	}
	labels := map[string]string{"app": "test"}

	secret, err := client.EnsureSecret("test-ns", "test-secret", data, labels)

	require.NoError(t, err)
	assert.NotNil(t, secret)
	assert.Equal(t, "test-secret", secret.Name)
	assert.Equal(t, data, secret.Data)
	assert.Equal(t, labels, secret.Labels)

	// Verify it was created
	createdSecret, err := fakeClient.CoreV1().Secrets("test-ns").Get(
		context.Background(), "test-secret", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, data, createdSecret.Data)
}

// TestClient_EnsureSecret_Update tests EnsureSecret when Secret exists
func TestClient_EnsureSecret_Update(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	// Create existing Secret
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			"key": []byte("oldvalue"),
		},
		Type: corev1.SecretTypeOpaque,
	}
	_, err := fakeClient.CoreV1().Secrets("test-ns").Create(
		context.Background(), existingSecret, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Ensure with new data
	newData := map[string][]byte{
		"key":  []byte("newvalue"),
		"key2": []byte("value2"),
	}
	labels := map[string]string{"app": "updated"}

	secret, err := client.EnsureSecret("test-ns", "test-secret", newData, labels)

	require.NoError(t, err)
	assert.NotNil(t, secret)
	assert.Equal(t, newData, secret.Data)

	// Verify it was updated
	updatedSecret, err := fakeClient.CoreV1().Secrets("test-ns").Get(
		context.Background(), "test-secret", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, newData, updatedSecret.Data)
}

// TestClient_EnsureSecret_NoChange tests EnsureSecret when data matches
func TestClient_EnsureSecret_NoChange(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	data := map[string][]byte{
		"key": []byte("value"),
	}

	// Create existing Secret
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			"key": []byte("value"),
		},
		Type: corev1.SecretTypeOpaque,
	}
	_, err := fakeClient.CoreV1().Secrets("test-ns").Create(
		context.Background(), existingSecret, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Ensure with same data
	secret, err := client.EnsureSecret("test-ns", "test-secret", data, nil)

	require.NoError(t, err)
	assert.NotNil(t, secret)
	assert.Equal(t, data, secret.Data)
}

// TestClient_Secret_SensitiveData tests that secret data is handled correctly
func TestClient_Secret_SensitiveData(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	// Test with various sensitive data types
	data := map[string][]byte{
		"password":      []byte("super-secret-password"),
		"api-key":       []byte("api-key-12345"),
		"certificate":   []byte("-----BEGIN CERTIFICATE-----\nMIIC..."),
		"private-key":   []byte("-----BEGIN PRIVATE KEY-----\nMIIE..."),
		"token":         []byte("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."),
		"empty-value":   []byte(""),
		"special-chars": []byte("!@#$%^&*(){}[]|\\:;\"'<>,.?/~`"),
	}

	secret, err := client.CreateSecret("test-ns", "sensitive-secret", data, nil)

	require.NoError(t, err)
	assert.NotNil(t, secret)

	// Verify all data was stored correctly
	for key, value := range data {
		assert.Equal(t, value, secret.Data[key], "Data for key %s should match", key)
	}
}
