package restorelock

import (
	"testing"

	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFormatConflictError(t *testing.T) {
	tests := []struct {
		name     string
		contains []string
	}{
		{
			name: "same datastore conflict",
			contains: []string{
				"cannot start elasticsearch restore",
				"another elasticsearch restore is already in progress",
				"2025-01-01T12:00:00Z",
				"Deployment/api-server",
				"To manually remove a stuck restore lock",
				"kubectl annotate",
			},
		},
		{
			name: "mutual exclusion conflict",
			contains: []string{
				"cannot start settings restore",
				"stackgraph restore is in progress",
				"mutually exclusive",
				"To manually remove a stuck restore lock",
			},
		},
	}

	t.Run(tests[0].name, func(t *testing.T) {
		err := formatConflictError(
			config.DatastoreElasticsearch, config.DatastoreElasticsearch,
			"Deployment", "api-server", "2025-01-01T12:00:00Z",
			false,
			"test-ns", "app=api",
		)
		errMsg := err.Error()
		for _, s := range tests[0].contains {
			assert.Contains(t, errMsg, s)
		}
	})

	t.Run(tests[1].name, func(t *testing.T) {
		err := formatConflictError(
			config.DatastoreSettings, config.DatastoreStackgraph,
			"Deployment", "server", "2025-01-01T12:00:00Z",
			true,
			"test-ns", "app=stackgraph",
		)
		errMsg := err.Error()
		for _, s := range tests[1].contains {
			assert.Contains(t, errMsg, s)
		}
	})
}

func TestCheckForConflicts_NoConflict(t *testing.T) {
	// Create fake clientset with deployment without lock annotation
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-server",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app": "api",
			},
		},
	}

	fakeClientset := fake.NewSimpleClientset(deployment)
	k8sClient := k8s.NewTestClient(fakeClientset)
	log := logger.New(false, true)

	allSelectors := LabelSelectors{
		config.DatastoreElasticsearch: "app=api",
		config.DatastoreStackgraph:    "app=stackgraph",
		config.DatastoreSettings:      "app=settings",
	}

	err := CheckForConflicts(k8sClient, "test-ns", config.DatastoreElasticsearch, allSelectors, log)
	assert.NoError(t, err)
}

func TestCheckForConflicts_SameDatastoreConflict(t *testing.T) {
	// Create fake clientset with deployment that has lock annotation
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-server",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app": "api",
			},
			Annotations: map[string]string{
				k8s.RestoreInProgressAnnotation: config.DatastoreElasticsearch,
				k8s.RestoreStartedAtAnnotation:  "2025-01-01T12:00:00Z",
			},
		},
	}

	fakeClientset := fake.NewSimpleClientset(deployment)
	k8sClient := k8s.NewTestClient(fakeClientset)
	log := logger.New(false, true)

	allSelectors := LabelSelectors{
		config.DatastoreElasticsearch: "app=api",
		config.DatastoreStackgraph:    "app=stackgraph",
		config.DatastoreSettings:      "app=settings",
	}

	err := CheckForConflicts(k8sClient, "test-ns", config.DatastoreElasticsearch, allSelectors, log)
	require.Error(t, err)

	errMsg := err.Error()
	assert.Contains(t, errMsg, "cannot start elasticsearch restore")
	assert.Contains(t, errMsg, "another elasticsearch restore is already in progress")
	assert.Contains(t, errMsg, "Deployment/api-server")
	assert.Contains(t, errMsg, "To manually remove a stuck restore lock")
}

func TestCheckForConflicts_MutualExclusionConflict(t *testing.T) {
	// Create fake clientset with stackgraph deployment that has lock annotation
	stackgraphDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stackgraph-server",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app": "stackgraph",
			},
			Annotations: map[string]string{
				k8s.RestoreInProgressAnnotation: config.DatastoreStackgraph,
				k8s.RestoreStartedAtAnnotation:  "2025-01-01T12:00:00Z",
			},
		},
	}

	// Settings deployment without lock (the one being checked)
	settingsDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "settings-server",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app": "settings",
			},
		},
	}

	fakeClientset := fake.NewSimpleClientset(stackgraphDeployment, settingsDeployment)
	k8sClient := k8s.NewTestClient(fakeClientset)
	log := logger.New(false, true)

	allSelectors := LabelSelectors{
		config.DatastoreElasticsearch: "app=elasticsearch",
		config.DatastoreStackgraph:    "app=stackgraph",
		config.DatastoreSettings:      "app=settings",
	}

	// Try to start settings restore when stackgraph is running
	err := CheckForConflicts(k8sClient, "test-ns", config.DatastoreSettings, allSelectors, log)
	require.Error(t, err)

	errMsg := err.Error()
	assert.Contains(t, errMsg, "cannot start settings restore")
	assert.Contains(t, errMsg, "stackgraph restore is in progress")
	assert.Contains(t, errMsg, "mutually exclusive")
	assert.Contains(t, errMsg, "To manually remove a stuck restore lock")
}

func TestCheckForConflicts_NoMutualExclusionBetweenIndependentDatastores(t *testing.T) {
	// Create fake clientset with elasticsearch deployment that has lock annotation
	esDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "es-master",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app": "elasticsearch",
			},
			Annotations: map[string]string{
				k8s.RestoreInProgressAnnotation: config.DatastoreElasticsearch,
				k8s.RestoreStartedAtAnnotation:  "2025-01-01T12:00:00Z",
			},
		},
	}

	// Clickhouse deployment without lock
	chDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clickhouse",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app": "clickhouse",
			},
		},
	}

	fakeClientset := fake.NewSimpleClientset(esDeployment, chDeployment)
	k8sClient := k8s.NewTestClient(fakeClientset)
	log := logger.New(false, true)

	allSelectors := LabelSelectors{
		config.DatastoreElasticsearch: "app=elasticsearch",
		config.DatastoreClickhouse:    "app=clickhouse",
		config.DatastoreStackgraph:    "app=stackgraph",
		config.DatastoreSettings:      "app=settings",
	}

	// Clickhouse restore should succeed even though elasticsearch is running
	err := CheckForConflicts(k8sClient, "test-ns", config.DatastoreClickhouse, allSelectors, log)
	assert.NoError(t, err)
}

func TestAcquireLock(t *testing.T) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-server",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app": "api",
			},
		},
	}

	fakeClientset := fake.NewSimpleClientset(deployment)
	k8sClient := k8s.NewTestClient(fakeClientset)
	log := logger.New(false, true)

	err := AcquireLock(k8sClient, "test-ns", "app=api", config.DatastoreElasticsearch, log)
	require.NoError(t, err)

	// Verify lock was set
	locks, err := k8sClient.GetRestoreLocks("test-ns", "app=api")
	require.NoError(t, err)
	require.Len(t, locks, 1)
	assert.Equal(t, config.DatastoreElasticsearch, locks[0].Datastore)
	assert.Equal(t, "Deployment", locks[0].ResourceKind)
	assert.Equal(t, "api-server", locks[0].ResourceName)
	assert.NotEmpty(t, locks[0].StartedAt)
}

func TestReleaseLock(t *testing.T) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-server",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app": "api",
			},
			Annotations: map[string]string{
				k8s.RestoreInProgressAnnotation: config.DatastoreElasticsearch,
				k8s.RestoreStartedAtAnnotation:  "2025-01-01T12:00:00Z",
			},
		},
	}

	fakeClientset := fake.NewSimpleClientset(deployment)
	k8sClient := k8s.NewTestClient(fakeClientset)
	log := logger.New(false, true)

	// Verify lock exists
	locks, err := k8sClient.GetRestoreLocks("test-ns", "app=api")
	require.NoError(t, err)
	require.Len(t, locks, 1)

	// Release lock
	err = ReleaseLock(k8sClient, "test-ns", "app=api", log)
	require.NoError(t, err)

	// Verify lock was removed
	locks, err = k8sClient.GetRestoreLocks("test-ns", "app=api")
	require.NoError(t, err)
	assert.Empty(t, locks)
}

func TestCheckForConflicts_StatefulSetLock(t *testing.T) {
	// Create fake clientset with statefulset that has lock annotation
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "victoria-metrics",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app": "vm",
			},
			Annotations: map[string]string{
				k8s.RestoreInProgressAnnotation: config.DatastoreVictoriaMetrics,
				k8s.RestoreStartedAtAnnotation:  "2025-01-01T12:00:00Z",
			},
		},
	}

	fakeClientset := fake.NewSimpleClientset(statefulSet)
	k8sClient := k8s.NewTestClient(fakeClientset)
	log := logger.New(false, true)

	allSelectors := LabelSelectors{
		config.DatastoreVictoriaMetrics: "app=vm",
	}

	err := CheckForConflicts(k8sClient, "test-ns", config.DatastoreVictoriaMetrics, allSelectors, log)
	require.Error(t, err)

	errMsg := err.Error()
	assert.Contains(t, errMsg, "StatefulSet/victoria-metrics")
	assert.Contains(t, errMsg, "To manually remove a stuck restore lock")
}
