package k8s

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// defaultJobTTLSeconds is the time-to-live for completed/failed jobs (1 day)
	defaultJobTTLSeconds = 86400
)

// BackupJobSpec contains all parameters needed to create a backup/restore job
type BackupJobSpec struct {
	// Job metadata
	Name   string
	Labels map[string]string

	// Pod spec parameters (using native k8s types)
	ImagePullSecrets         []corev1.LocalObjectReference
	SecurityContext          *corev1.PodSecurityContext
	NodeSelector             map[string]string
	Tolerations              []corev1.Toleration
	Affinity                 *corev1.Affinity
	ContainerSecurityContext *corev1.SecurityContext

	// Container spec
	Containers     []corev1.Container
	InitContainers []corev1.Container

	// Volumes
	Volumes []corev1.Volume

	// PVC spec (optional - only for jobs that need persistent storage)
	// If nil, no PVC will be created
	PVCSpec *PVCSpec
}

// PVCSpec contains parameters for creating a PersistentVolumeClaim
// NOTE: Some backup types (e.g., Stackgraph) require a PVC for temporary storage,
// while others (e.g., Elasticsearch, VictoriaMetrics, Clickhouse) may not.
// Set this to nil if the job doesn't require persistent storage.
type PVCSpec struct {
	Name         string
	Labels       map[string]string
	StorageSize  string // e.g., "10Gi"
	AccessModes  []corev1.PersistentVolumeAccessMode
	StorageClass *string // nil for default storage class
}

// CreatePVC creates a PersistentVolumeClaim
func (c *Client) CreatePVC(namespace string, spec PVCSpec) (*corev1.PersistentVolumeClaim, error) {
	ctx := context.Background()

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   spec.Name,
			Labels: spec.Labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: spec.AccessModes,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(spec.StorageSize),
				},
			},
		},
	}

	if spec.StorageClass != nil {
		pvc.Spec.StorageClassName = spec.StorageClass
	}

	createdPVC, err := c.clientset.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create PVC: %w", err)
	}

	return createdPVC, nil
}

// CreateBackupJob creates a Kubernetes Job for backup/restore operations
// Note: PVC must be created separately if needed using CreatePVC
// Returns the created Job and any error
func (c *Client) CreateBackupJob(namespace string, spec BackupJobSpec) (*batchv1.Job, error) {
	ctx := context.Background()

	// Build Job spec
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:   spec.Name,
			Labels: spec.Labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr(int32(1)),
			TTLSecondsAfterFinished: ptr(int32(defaultJobTTLSeconds)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: spec.Labels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy:  corev1.RestartPolicyNever,
					InitContainers: spec.InitContainers,
					Containers:     spec.Containers,
					Volumes:        spec.Volumes,
				},
			},
		},
	}

	// Apply security context if provided
	if spec.SecurityContext != nil {
		job.Spec.Template.Spec.SecurityContext = spec.SecurityContext
	}

	// Apply node selector if provided
	if len(spec.NodeSelector) > 0 {
		job.Spec.Template.Spec.NodeSelector = spec.NodeSelector
	}

	// Apply tolerations if provided
	if len(spec.Tolerations) > 0 {
		job.Spec.Template.Spec.Tolerations = spec.Tolerations
	}

	// Apply affinity if provided
	if spec.Affinity != nil {
		job.Spec.Template.Spec.Affinity = spec.Affinity
	}

	// Apply image pull secrets if provided
	if len(spec.ImagePullSecrets) > 0 {
		job.Spec.Template.Spec.ImagePullSecrets = spec.ImagePullSecrets
	}

	// Create Job
	createdJob, err := c.clientset.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	return createdJob, nil
}

// DeleteJob deletes a Kubernetes Job with Background propagation policy
// This ensures child pods are automatically cleaned up when the job is deleted
func (c *Client) DeleteJob(namespace, name string) error {
	ctx := context.Background()
	propagationPolicy := metav1.DeletePropagationBackground
	return c.clientset.BatchV1().Jobs(namespace).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
}

// DeletePVC deletes a PersistentVolumeClaim
func (c *Client) DeletePVC(namespace, name string) error {
	ctx := context.Background()
	return c.clientset.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// GetJob retrieves a Kubernetes Job
func (c *Client) GetJob(namespace, name string) (*batchv1.Job, error) {
	ctx := context.Background()
	return c.clientset.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
}

// ptr returns a pointer to the provided value
func ptr[T any](v T) *T {
	return &v
}
