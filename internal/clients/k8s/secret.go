package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateSecret creates a Secret with the provided data
func (c *Client) CreateSecret(namespace, name string, data map[string][]byte, labels map[string]string) (*corev1.Secret, error) {
	ctx := context.Background()

	if len(data) == 0 {
		return nil, fmt.Errorf("no data provided for Secret")
	}

	// Create default labels if none provided
	if labels == nil {
		labels = make(map[string]string)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Data: data,
		Type: corev1.SecretTypeOpaque,
	}

	created, err := c.clientset.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create Secret: %w", err)
	}

	return created, nil
}

// GetSecret retrieves a Secret by name
func (c *Client) GetSecret(namespace, name string) (*corev1.Secret, error) {
	ctx := context.Background()
	return c.clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
}

// UpdateSecret updates an existing Secret with new data
func (c *Client) UpdateSecret(namespace, name string, data map[string][]byte) (*corev1.Secret, error) {
	ctx := context.Background()

	if len(data) == 0 {
		return nil, fmt.Errorf("no data provided for Secret update")
	}

	// Get existing Secret
	existing, err := c.GetSecret(namespace, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get existing Secret: %w", err)
	}

	// Update data
	existing.Data = data

	updated, err := c.clientset.CoreV1().Secrets(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update Secret: %w", err)
	}

	return updated, nil
}

// DeleteSecret deletes a Secret by name
func (c *Client) DeleteSecret(namespace, name string) error {
	ctx := context.Background()
	return c.clientset.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// EnsureSecret ensures a Secret exists with the provided data, creating or updating it as needed
func (c *Client) EnsureSecret(namespace, name string, data map[string][]byte, labels map[string]string) (*corev1.Secret, error) {
	// Try to get existing Secret
	_, err := c.GetSecret(namespace, name)
	if err == nil {
		// Secret exists, update it
		return c.UpdateSecret(namespace, name, data)
	}

	// Secret doesn't exist, create it
	return c.CreateSecret(namespace, name, data, labels)
}
