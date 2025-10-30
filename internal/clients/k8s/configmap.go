package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateConfigMap creates a ConfigMap with the provided data
func (c *Client) CreateConfigMap(namespace, name string, data map[string]string, labels map[string]string) (*corev1.ConfigMap, error) {
	ctx := context.Background()

	if len(data) == 0 {
		return nil, fmt.Errorf("no data provided for ConfigMap")
	}

	// Create default labels if none provided
	if labels == nil {
		labels = make(map[string]string)
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Data: data,
	}

	created, err := c.clientset.CoreV1().ConfigMaps(namespace).Create(ctx, configMap, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create ConfigMap: %w", err)
	}

	return created, nil
}

// GetConfigMap retrieves a ConfigMap by name
func (c *Client) GetConfigMap(namespace, name string) (*corev1.ConfigMap, error) {
	ctx := context.Background()
	return c.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
}

// UpdateConfigMap updates an existing ConfigMap with new data
func (c *Client) UpdateConfigMap(namespace, name string, data map[string]string) (*corev1.ConfigMap, error) {
	ctx := context.Background()

	if len(data) == 0 {
		return nil, fmt.Errorf("no data provided for ConfigMap update")
	}

	// Get existing ConfigMap
	existing, err := c.GetConfigMap(namespace, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get existing ConfigMap: %w", err)
	}

	// Update data
	existing.Data = data

	updated, err := c.clientset.CoreV1().ConfigMaps(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update ConfigMap: %w", err)
	}

	return updated, nil
}

// DeleteConfigMap deletes a ConfigMap by name
func (c *Client) DeleteConfigMap(namespace, name string) error {
	ctx := context.Background()
	return c.clientset.CoreV1().ConfigMaps(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// EnsureConfigMap ensures a ConfigMap exists with the provided data, creating or updating it as needed
func (c *Client) EnsureConfigMap(namespace, name string, data map[string]string, labels map[string]string) (*corev1.ConfigMap, error) {
	// Try to get existing ConfigMap
	_, err := c.GetConfigMap(namespace, name)
	if err == nil {
		// ConfigMap exists, update it
		return c.UpdateConfigMap(namespace, name, data)
	}

	// ConfigMap doesn't exist, create it
	return c.CreateConfigMap(namespace, name, data, labels)
}
