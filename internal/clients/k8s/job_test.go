package k8s

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestClient_CreatePVC tests PersistentVolumeClaim creation
func TestClient_CreatePVC(t *testing.T) {
	tests := []struct {
		name         string
		namespace    string
		spec         PVCSpec
		expectError  bool
		validateFunc func(*testing.T, *corev1.PersistentVolumeClaim)
	}{
		{
			name:      "create PVC with default storage class",
			namespace: "test-ns",
			spec: PVCSpec{
				Name:        "test-pvc",
				Labels:      map[string]string{"app": "backup"},
				StorageSize: "10Gi",
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			},
			expectError: false,
			validateFunc: func(t *testing.T, pvc *corev1.PersistentVolumeClaim) {
				assert.Equal(t, "test-pvc", pvc.Name)
				assert.Equal(t, map[string]string{"app": "backup"}, pvc.Labels)
				assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, pvc.Spec.AccessModes)
				assert.Nil(t, pvc.Spec.StorageClassName)
			},
		},
		{
			name:      "create PVC with custom storage class",
			namespace: "test-ns",
			spec: PVCSpec{
				Name:         "test-pvc-custom",
				Labels:       map[string]string{"type": "restore"},
				StorageSize:  "20Gi",
				AccessModes:  []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
				StorageClass: ptr("fast-ssd"),
			},
			expectError: false,
			validateFunc: func(t *testing.T, pvc *corev1.PersistentVolumeClaim) {
				assert.Equal(t, "test-pvc-custom", pvc.Name)
				assert.NotNil(t, pvc.Spec.StorageClassName)
				assert.Equal(t, "fast-ssd", *pvc.Spec.StorageClassName)
			},
		},
		{
			name:      "create PVC with multiple access modes",
			namespace: "test-ns",
			spec: PVCSpec{
				Name:        "multi-mode-pvc",
				StorageSize: "5Gi",
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
					corev1.ReadOnlyMany,
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, pvc *corev1.PersistentVolumeClaim) {
				assert.Len(t, pvc.Spec.AccessModes, 2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset()
			client := &Client{clientset: fakeClient}

			pvc, err := client.CreatePVC(tt.namespace, tt.spec)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, pvc)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, pvc)
				if tt.validateFunc != nil {
					tt.validateFunc(t, pvc)
				}

				// Verify PVC was actually created in fake client
				createdPVC, err := fakeClient.CoreV1().PersistentVolumeClaims(tt.namespace).Get(
					context.Background(), tt.spec.Name, metav1.GetOptions{},
				)
				require.NoError(t, err)
				assert.Equal(t, tt.spec.Name, createdPVC.Name)
			}
		})
	}
}

// TestClient_CreateBackupJob tests Job creation for backup/restore operations
//
//nolint:funlen
func TestClient_CreateBackupJob(t *testing.T) {
	tests := []struct {
		name         string
		namespace    string
		spec         BackupJobSpec
		expectError  bool
		validateFunc func(*testing.T, *batchv1.Job)
	}{
		{
			name:      "create minimal job",
			namespace: "test-ns",
			spec: BackupJobSpec{
				Name:   "backup-job",
				Labels: map[string]string{"app": "backup"},
				Containers: []corev1.Container{
					{
						Name:  "backup",
						Image: "backup:latest",
					},
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, job *batchv1.Job) {
				assert.Equal(t, "backup-job", job.Name)
				assert.Equal(t, map[string]string{"app": "backup"}, job.Labels)
				assert.Equal(t, int32(1), *job.Spec.BackoffLimit)
				assert.Equal(t, int32(defaultJobTTLSeconds), *job.Spec.TTLSecondsAfterFinished)
				assert.Equal(t, corev1.RestartPolicyNever, job.Spec.Template.Spec.RestartPolicy)
			},
		},
		{
			name:      "create job with environment variables",
			namespace: "test-ns",
			spec: BackupJobSpec{
				Name: "restore-job",
				Containers: []corev1.Container{
					{
						Name:  "restore",
						Image: "restore:v1",
						Env: []corev1.EnvVar{
							{Name: "BACKUP_NAME", Value: "snapshot-123"},
							{Name: "LOG_LEVEL", Value: "debug"},
						},
					},
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, job *batchv1.Job) {
				assert.Len(t, job.Spec.Template.Spec.Containers, 1)
				assert.Len(t, job.Spec.Template.Spec.Containers[0].Env, 2)
				assert.Equal(t, "BACKUP_NAME", job.Spec.Template.Spec.Containers[0].Env[0].Name)
			},
		},
		{
			name:      "create job with resource requirements",
			namespace: "test-ns",
			spec: BackupJobSpec{
				Name: "resource-job",
				Containers: []corev1.Container{
					{
						Name:  "backup",
						Image: "backup:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("2"),
								corev1.ResourceMemory: resource.MustParse("4Gi"),
							},
						},
					},
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, job *batchv1.Job) {
				resources := job.Spec.Template.Spec.Containers[0].Resources
				assert.NotNil(t, resources.Requests)
				assert.NotNil(t, resources.Limits)
			},
		},
		{
			name:      "create job with init containers",
			namespace: "test-ns",
			spec: BackupJobSpec{
				Name: "init-job",
				Containers: []corev1.Container{
					{
						Name:  "main",
						Image: "main:latest",
					},
				},
				InitContainers: []corev1.Container{
					{
						Name:    "wait-for-deps",
						Image:   "wait:latest",
						Command: []string{"/wait.sh"},
					},
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, job *batchv1.Job) {
				assert.Len(t, job.Spec.Template.Spec.InitContainers, 1)
				assert.Equal(t, "wait-for-deps", job.Spec.Template.Spec.InitContainers[0].Name)
			},
		},
		{
			name:      "create job with volumes and mounts",
			namespace: "test-ns",
			spec: BackupJobSpec{
				Name: "volume-job",
				Containers: []corev1.Container{
					{
						Name:  "backup",
						Image: "backup:latest",
						VolumeMounts: []corev1.VolumeMount{
							{Name: "data", MountPath: "/data"},
							{Name: "config", MountPath: "/config"},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "data-pvc",
							},
						},
					},
					{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "backup-config",
								},
							},
						},
					},
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, job *batchv1.Job) {
				assert.Len(t, job.Spec.Template.Spec.Volumes, 2)
				assert.Len(t, job.Spec.Template.Spec.Containers[0].VolumeMounts, 2)
			},
		},
		{
			name:      "create job with security context",
			namespace: "test-ns",
			spec: BackupJobSpec{
				Name: "secure-job",
				Containers: []corev1.Container{
					{
						Name:  "backup",
						Image: "backup:latest",
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr(false),
							ReadOnlyRootFilesystem:   ptr(true),
						},
					},
				},
				SecurityContext: &corev1.PodSecurityContext{
					RunAsUser:  ptr(int64(1000)),
					RunAsGroup: ptr(int64(2000)),
					FSGroup:    ptr(int64(3000)),
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, job *batchv1.Job) {
				assert.NotNil(t, job.Spec.Template.Spec.SecurityContext)
				assert.Equal(t, int64(1000), *job.Spec.Template.Spec.SecurityContext.RunAsUser)
				assert.NotNil(t, job.Spec.Template.Spec.Containers[0].SecurityContext)
				assert.False(t, *job.Spec.Template.Spec.Containers[0].SecurityContext.AllowPrivilegeEscalation)
			},
		},
		{
			name:      "create job with node selector and tolerations",
			namespace: "test-ns",
			spec: BackupJobSpec{
				Name: "scheduled-job",
				Containers: []corev1.Container{
					{
						Name:  "backup",
						Image: "backup:latest",
					},
				},
				NodeSelector: map[string]string{
					"disktype": "ssd",
					"zone":     "us-west-1a",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "key1",
						Operator: corev1.TolerationOpEqual,
						Value:    "value1",
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, job *batchv1.Job) {
				assert.Len(t, job.Spec.Template.Spec.NodeSelector, 2)
				assert.Len(t, job.Spec.Template.Spec.Tolerations, 1)
				assert.Equal(t, "key1", job.Spec.Template.Spec.Tolerations[0].Key)
			},
		},
		{
			name:      "create job with image pull secrets",
			namespace: "test-ns",
			spec: BackupJobSpec{
				Name: "private-image-job",
				Containers: []corev1.Container{
					{
						Name:  "backup",
						Image: "private-registry.com/backup:latest",
					},
				},
				ImagePullSecrets: []corev1.LocalObjectReference{
					{Name: "registry-secret"},
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, job *batchv1.Job) {
				assert.Len(t, job.Spec.Template.Spec.ImagePullSecrets, 1)
				assert.Equal(t, "registry-secret", job.Spec.Template.Spec.ImagePullSecrets[0].Name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset()
			client := &Client{clientset: fakeClient}

			job, err := client.CreateBackupJob(tt.namespace, tt.spec)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, job)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, job)
				if tt.validateFunc != nil {
					tt.validateFunc(t, job)
				}

				// Verify job was actually created in fake client
				createdJob, err := fakeClient.BatchV1().Jobs(tt.namespace).Get(
					context.Background(), tt.spec.Name, metav1.GetOptions{},
				)
				require.NoError(t, err)
				assert.Equal(t, tt.spec.Name, createdJob.Name)
			}
		})
	}
}

// TestClient_DeleteJob tests Job deletion
func TestClient_DeleteJob(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	// Create a job first
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "test-ns",
		},
	}
	_, err := fakeClient.BatchV1().Jobs("test-ns").Create(
		context.Background(), job, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Delete the job
	err = client.DeleteJob("test-ns", "test-job")
	assert.NoError(t, err)

	// Verify job was deleted
	_, err = fakeClient.BatchV1().Jobs("test-ns").Get(
		context.Background(), "test-job", metav1.GetOptions{},
	)
	assert.Error(t, err)
}

// TestClient_DeletePVC tests PVC deletion
func TestClient_DeletePVC(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	// Create a PVC first
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pvc",
			Namespace: "test-ns",
		},
	}
	_, err := fakeClient.CoreV1().PersistentVolumeClaims("test-ns").Create(
		context.Background(), pvc, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Delete the PVC
	err = client.DeletePVC("test-ns", "test-pvc")
	assert.NoError(t, err)

	// Verify PVC was deleted
	_, err = fakeClient.CoreV1().PersistentVolumeClaims("test-ns").Get(
		context.Background(), "test-pvc", metav1.GetOptions{},
	)
	assert.Error(t, err)
}

// TestClient_GetJob tests Job retrieval
func TestClient_GetJob(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	// Create a job
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "test-ns",
			Labels:    map[string]string{"app": "backup"},
		},
	}
	_, err := fakeClient.BatchV1().Jobs("test-ns").Create(
		context.Background(), job, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Get the job
	retrievedJob, err := client.GetJob("test-ns", "test-job")
	assert.NoError(t, err)
	assert.NotNil(t, retrievedJob)
	assert.Equal(t, "test-job", retrievedJob.Name)
	assert.Equal(t, map[string]string{"app": "backup"}, retrievedJob.Labels)
}

// TestClient_GetJob_NotFound tests error case when job doesn't exist
func TestClient_GetJob_NotFound(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	client := &Client{clientset: fakeClient}

	_, err := client.GetJob("test-ns", "nonexistent-job")
	assert.Error(t, err)
}

// TestClient_DefaultJobTTL tests the default TTL constant
func TestClient_DefaultJobTTL(t *testing.T) {
	assert.Equal(t, int32(86400), int32(defaultJobTTLSeconds))
}
