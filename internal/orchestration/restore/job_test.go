package restore

import (
	"context"
	"testing"
	"time"

	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	testNamespace = "test-ns"
	testJobName   = "test-job"
)

// newTestJob creates a minimal batchv1.Job with the given status values.
func newTestJob(succeeded, failed int32) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testJobName,
			Namespace: testNamespace,
		},
		Status: batchv1.JobStatus{
			Succeeded: succeeded,
			Failed:    failed,
		},
	}
}

// updateJobStatus writes an updated status back via Update (not UpdateStatus) so that
// the fake client's object tracker reflects the change on the next Get call.
func updateJobStatus(t *testing.T, fakeClient *fake.Clientset, succeeded, failed int32) {
	t.Helper()
	job, err := fakeClient.BatchV1().Jobs(testNamespace).Get(context.Background(), testJobName, metav1.GetOptions{})
	require.NoError(t, err)
	job.Status.Succeeded = succeeded
	job.Status.Failed = failed
	_, err = fakeClient.BatchV1().Jobs(testNamespace).Update(context.Background(), job, metav1.UpdateOptions{})
	require.NoError(t, err)
}

// TestWaitForJobCompletion_Success verifies that WaitForJobCompletion returns nil when the
// job is already in a succeeded state on the first poll.
func TestWaitForJobCompletion_Success(t *testing.T) {
	// Pre-populate with a job that has already succeeded
	fakeClient := fake.NewSimpleClientset(newTestJob(1, 0))
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false)

	err := WaitForJobCompletion(client, testNamespace, testJobName, log, 5*time.Second)
	require.NoError(t, err)
}

// TestWaitForJobCompletion_JobFailed verifies that WaitForJobCompletion returns an error
// when the job is already in a failed state.
func TestWaitForJobCompletion_JobFailed(t *testing.T) {
	// Pre-populate with a job that has already failed
	fakeClient := fake.NewSimpleClientset(newTestJob(0, 1))
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false)

	err := WaitForJobCompletion(client, testNamespace, testJobName, log, 5*time.Second)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "job failed")
}

// TestWaitForJobCompletion_Timeout verifies that WaitForJobCompletion returns a timeout
// error when the job remains pending for the full duration.
func TestWaitForJobCompletion_Timeout(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(newTestJob(0, 0))
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false)

	// Very short timeout; job never transitions
	err := WaitForJobCompletion(client, testNamespace, testJobName, log, 100*time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout waiting for job to complete")
}

// TestWaitForJobCompletion_JobNotFound verifies that WaitForJobCompletion returns an error
// immediately when the job cannot be found (first Get returns 404).
func TestWaitForJobCompletion_JobNotFound(t *testing.T) {
	// Empty fake client — no job pre-created
	fakeClient := fake.NewSimpleClientset()
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false)

	// The first poll will fail to find the job; give enough timeout so we're not racing
	err := WaitForJobCompletion(client, testNamespace, testJobName, log, 5*time.Second)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get job status")
}

// TestWaitForJobCompletion_DefaultTimeout verifies the value of the fallback constant.
func TestWaitForJobCompletion_DefaultTimeout(t *testing.T) {
	assert.Equal(t, 30*time.Minute, defaultJobCompletionTimeout)
}

// TestWaitForJobCompletion_CustomTimeout verifies that a caller-supplied short timeout
// is respected: the function returns quickly and produces the right error.
func TestWaitForJobCompletion_CustomTimeout(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(newTestJob(0, 0))
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false)

	customTimeout := 150 * time.Millisecond
	start := time.Now()

	err := WaitForJobCompletion(client, testNamespace, testJobName, log, customTimeout)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout waiting for job to complete")
	// Should have returned near customTimeout, well under 5 seconds
	assert.Less(t, elapsed, 5*time.Second, "returned much later than the custom timeout")
}

// TestWaitForJobCompletion_SuccessAfterPending verifies that the function keeps polling
// and detects a job that transitions to succeeded after a short delay.
func TestWaitForJobCompletion_SuccessAfterPending(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(newTestJob(0, 0))
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false)

	// Mark the job as succeeded after one polling interval (10s) would have fired;
	// but since we poll with a ticker the test just needs status set before the timeout.
	// We set it in a goroutine shortly after the function starts.
	go func() {
		time.Sleep(200 * time.Millisecond)
		updateJobStatus(t, fakeClient, 1, 0)
	}()

	// Allow plenty of time — function should return early once the status is updated
	err := WaitForJobCompletion(client, testNamespace, testJobName, log, 30*time.Second)
	require.NoError(t, err)
}

// TestWaitAndCleanup_Success verifies the full wait-and-cleanup path when the job
// is already in a succeeded state.
func TestWaitAndCleanup_Success(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(newTestJob(1, 0))
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false)

	err := WaitAndCleanup(client, testNamespace, testJobName, log, false, 5*time.Second)
	require.NoError(t, err)
}

// TestWaitAndCleanup_Timeout verifies that WaitAndCleanup propagates a timeout error.
func TestWaitAndCleanup_Timeout(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(newTestJob(0, 0))
	client := k8s.NewTestClient(fakeClient)
	log := logger.New(true, false)

	err := WaitAndCleanup(client, testNamespace, testJobName, log, false, 100*time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout waiting for job to complete")
}
