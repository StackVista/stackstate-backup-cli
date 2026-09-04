package elasticsearch

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stackvista/stackstate-backup-cli/internal/app"
	es "github.com/stackvista/stackstate-backup-cli/internal/clients/elasticsearch"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
)

type healthResult struct {
	health map[string]es.IndexHealth
	err    error
}

// fakeHealthClient replays results in order, repeating the last one once exhausted.
type fakeHealthClient struct {
	results []healthResult
	calls   int
}

type snapshotOnlyClient struct {
	es.Interface
	snapshot *es.Snapshot
}

func (c *snapshotOnlyClient) GetSnapshot(_, _ string) (*es.Snapshot, error) {
	return c.snapshot, nil
}

func (f *fakeHealthClient) GetIndicesHealth() (map[string]es.IndexHealth, error) {
	result := f.results[min(f.calls, len(f.results)-1)]
	f.calls++

	return result.health, result.err
}

func ok(health map[string]es.IndexHealth) healthResult {
	return healthResult{health: health}
}

func restored(shards int) es.IndexHealth {
	return es.IndexHealth{Status: "green", NumberOfShards: shards, ActivePrimaryShards: shards}
}

const testDatastreamPrefix = ".ds-sts_k8s_logs"

func TestMeasureRestore(t *testing.T) {
	tests := []struct {
		name          string
		expected      []string
		health        map[string]es.IndexHealth
		wantIndices   int
		wantPrimaries int
	}{
		{
			name:          "index not recreated yet",
			expected:      []string{"sts_topology", "sts_events"},
			health:        map[string]es.IndexHealth{"sts_topology": restored(1)},
			wantIndices:   1,
			wantPrimaries: 1,
		},
		{
			name:     "primaries still recovering are not counted",
			expected: []string{"sts_topology"},
			health: map[string]es.IndexHealth{
				"sts_topology": {Status: "red", NumberOfShards: 3, ActivePrimaryShards: 2, InitializingShards: 1},
			},
			wantIndices:   0,
			wantPrimaries: 2,
		},
		{
			name:     "index present with no active primaries is not counted",
			expected: []string{"sts_topology"},
			health: map[string]es.IndexHealth{
				"sts_topology": {Status: "red", NumberOfShards: 3, ActivePrimaryShards: 0, UnassignedShards: 3},
			},
			wantIndices:   0,
			wantPrimaries: 0,
		},
		{
			name:     "unassigned replicas do not block completion",
			expected: []string{"sts_topology"},
			health: map[string]es.IndexHealth{
				"sts_topology": {
					Status: "yellow", NumberOfShards: 1, NumberOfReplicas: 1,
					ActivePrimaryShards: 1, ActiveShards: 1, UnassignedShards: 1,
				},
			},
			wantIndices:   1,
			wantPrimaries: 1,
		},
		{
			name:     "all primaries active across several indices",
			expected: []string{"sts_topology", ".ds-sts_k8s_logs-000001"},
			health: map[string]es.IndexHealth{
				"sts_topology":            restored(3),
				".ds-sts_k8s_logs-000001": restored(2),
				"unrelated":               restored(1),
			},
			wantIndices:   2,
			wantPrimaries: 5,
		},
		{
			name: "oldest lifecycle-managed indices can be absent",
			expected: []string{
				".ds-sts_k8s_logs-000001",
				".ds-sts_k8s_logs-000002",
				".ds-sts_k8s_logs-000003",
				"sts_topology",
			},
			health: map[string]es.IndexHealth{
				".ds-sts_k8s_logs-000002": restored(2),
				".ds-sts_k8s_logs-000003": restored(2),
				"sts_topology":            restored(1),
			},
			wantIndices:   3,
			wantPrimaries: 5,
		},
		{
			name:          "index reporting zero shards is not counted",
			expected:      []string{"sts_topology"},
			health:        map[string]es.IndexHealth{"sts_topology": {Status: "green"}},
			wantIndices:   0,
			wantPrimaries: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			progress := measureRestore(tt.expected, testDatastreamPrefix, tt.health)
			assert.Equal(t, tt.wantIndices, progress.indicesRestored, "indicesRestored")
			assert.Equal(t, tt.wantPrimaries, progress.primariesActive, "primariesActive")
		})
	}
}

var statusFnExpected = []string{"sts_topology", "sts_events"}

func TestNewRestoreStatusFn_Progress(t *testing.T) {
	expected := statusFnExpected
	log := logger.New(true, false)

	t.Run("accepted but not yet applied reports in progress", func(t *testing.T) {
		client := &fakeHealthClient{results: []healthResult{ok(map[string]es.IndexHealth{})}}

		status, isComplete, err := newRestoreStatusFn(client, log, expected, testDatastreamPrefix, time.Minute, 5)()

		require.NoError(t, err)
		assert.Equal(t, es.StatusInProgress, status)
		assert.False(t, isComplete)
	})

	t.Run("complete once every index has all primaries active", func(t *testing.T) {
		client := &fakeHealthClient{results: []healthResult{
			ok(map[string]es.IndexHealth{"sts_topology": restored(1)}),
			ok(map[string]es.IndexHealth{"sts_topology": restored(1), "sts_events": restored(1)}),
		}}
		statusFn := newRestoreStatusFn(client, log, expected, testDatastreamPrefix, time.Minute, 5)

		status, isComplete, err := statusFn()
		require.NoError(t, err)
		assert.Equal(t, es.StatusInProgress, status)
		assert.False(t, isComplete)

		status, isComplete, err = statusFn()
		require.NoError(t, err)
		assert.Equal(t, es.StatusSuccess, status)
		assert.True(t, isComplete)
	})
}

func TestNewRestoreStatusFn_ILMRemovedOldestBackingIndices(t *testing.T) {
	expected := []string{
		"sts_topology",
		".ds-sts_k8s_logs-2026.08.28-012011",
		".ds-sts_k8s_logs-2026.08.26-011965",
		".ds-sts_k8s_logs-2026.08.27-011999",
	}
	health := map[string]es.IndexHealth{
		"sts_topology":                       restored(1),
		".ds-sts_k8s_logs-2026.08.27-011999": restored(1),
		".ds-sts_k8s_logs-2026.08.28-012011": restored(1),
	}

	status, isComplete, err := restoreStatus(expected, health)

	require.NoError(t, err)
	assert.Equal(t, es.StatusSuccess, status)
	assert.True(t, isComplete)
}

func TestNewRestoreStatusFn_ILMRemovedAllBackingIndices(t *testing.T) {
	expected := []string{"sts_topology", ".ds-sts_k8s_logs-2026.08.26-011965"}
	health := map[string]es.IndexHealth{"sts_topology": restored(1)}

	status, isComplete, err := restoreStatus(expected, health)

	require.NoError(t, err)
	assert.Equal(t, es.StatusSuccess, status)
	assert.True(t, isComplete)
}

func TestNewRestoreStatusFn_UnsafeMissingIndices(t *testing.T) {
	tests := []struct {
		name     string
		expected []string
		health   map[string]es.IndexHealth
	}{
		{
			name:     "ordinary index",
			expected: []string{"sts_topology", "sts_events", ".ds-sts_k8s_logs-000001"},
			health: map[string]es.IndexHealth{
				"sts_topology":            restored(1),
				".ds-sts_k8s_logs-000001": restored(1),
			},
		},
		{
			name: "backing index after a present generation",
			expected: []string{
				"sts_topology",
				".ds-sts_k8s_logs-000001",
				".ds-sts_k8s_logs-000002",
				".ds-sts_k8s_logs-000003",
			},
			health: map[string]es.IndexHealth{
				"sts_topology":            restored(1),
				".ds-sts_k8s_logs-000001": restored(1),
				".ds-sts_k8s_logs-000003": restored(1),
			},
		},
		{
			name:     "newest backing index",
			expected: []string{"sts_topology", ".ds-sts_k8s_logs-000001", ".ds-sts_k8s_logs-000002"},
			health: map[string]es.IndexHealth{
				"sts_topology":            restored(1),
				".ds-sts_k8s_logs-000001": restored(1),
			},
		},
		{
			name:     "snapshot has no required anchor",
			expected: []string{".ds-sts_k8s_logs-000001"},
			health:   map[string]es.IndexHealth{".ds-sts_k8s_logs-000001": restored(1)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, isComplete, err := restoreStatus(tt.expected, tt.health)

			require.NoError(t, err)
			assert.Equal(t, es.StatusInProgress, status)
			assert.False(t, isComplete)
		})
	}
}

func TestExpectedRestoredIndices_RejectsLifecycleOnlySnapshot(t *testing.T) {
	client := &snapshotOnlyClient{snapshot: &es.Snapshot{
		Snapshot: "snapshot",
		Indices:  []string{".ds-sts_k8s_logs-2026.08.28-012011"},
	}}
	appCtx := &app.Context{Config: &config.Config{
		Elasticsearch: config.ElasticsearchConfig{Restore: config.RestoreConfig{
			IndexPrefix:           "sts",
			DatastreamIndexPrefix: testDatastreamPrefix,
		}},
	}}

	_, err := expectedRestoredIndices(client, appCtx, "repository", "snapshot")

	require.Error(t, err)
	assert.ErrorContains(t, err, "only lifecycle-managed data-stream backing indices")
	assert.ErrorContains(t, err, "cannot be determined safely")
}

func restoreStatus(expected []string, health map[string]es.IndexHealth) (string, bool, error) {
	client := &fakeHealthClient{results: []healthResult{ok(health)}}
	log := logger.New(true, false)

	return newRestoreStatusFn(client, log, expected, testDatastreamPrefix, time.Minute, 5)()
}

func TestNewRestoreStatusFn_Deadline(t *testing.T) {
	expected := statusFnExpected
	log := logger.New(true, false)

	t.Run("stalled restore errors instead of polling forever", func(t *testing.T) {
		client := &fakeHealthClient{results: []healthResult{
			ok(map[string]es.IndexHealth{"sts_topology": restored(1)}),
		}}
		statusFn := newRestoreStatusFn(client, log, expected, testDatastreamPrefix, time.Nanosecond, 5)

		// First call records the progress it observed; the second sees no change past the deadline.
		_, isComplete, err := statusFn()
		require.NoError(t, err)
		assert.False(t, isComplete)

		_, isComplete, err = statusFn()
		require.Error(t, err)
		assert.False(t, isComplete)
		// Must not read as a failed restore: the caller's retry deletes every STS index first.
		assert.ErrorContains(t, err, "stalled")
		assert.ErrorContains(t, err, "may still be running")
		assert.NotContains(t, err.Error(), "restore failed")
	})

	t.Run("shard progress inside one index is not a stall", func(t *testing.T) {
		// A large multi-shard index can restore for a long time without completing. Counting whole
		// indices would read this as stalled; counting primaries does not.
		client := &fakeHealthClient{results: []healthResult{
			ok(map[string]es.IndexHealth{"sts_topology": {NumberOfShards: 5, ActivePrimaryShards: 1}}),
			ok(map[string]es.IndexHealth{"sts_topology": {NumberOfShards: 5, ActivePrimaryShards: 2}}),
			ok(map[string]es.IndexHealth{"sts_topology": {NumberOfShards: 5, ActivePrimaryShards: 3}}),
		}}
		statusFn := newRestoreStatusFn(client, log, expected, testDatastreamPrefix, time.Nanosecond, 5)

		for range 3 {
			status, isComplete, err := statusFn()
			require.NoError(t, err)
			assert.Equal(t, es.StatusInProgress, status)
			assert.False(t, isComplete)
		}
	})

	t.Run("progress resets the deadline", func(t *testing.T) {
		client := &fakeHealthClient{results: []healthResult{
			ok(map[string]es.IndexHealth{}),
			ok(map[string]es.IndexHealth{"sts_topology": restored(1)}),
		}}
		statusFn := newRestoreStatusFn(client, log, expected, testDatastreamPrefix, time.Nanosecond, 5)

		_, _, err := statusFn()
		require.NoError(t, err)

		status, isComplete, err := statusFn()
		require.NoError(t, err)
		assert.Equal(t, es.StatusInProgress, status)
		assert.False(t, isComplete)
	})

	t.Run("decreasing primaries do not reset the deadline", func(t *testing.T) {
		client := &fakeHealthClient{results: []healthResult{
			ok(map[string]es.IndexHealth{"sts_topology": {NumberOfShards: 4, ActivePrimaryShards: 3}}),
			ok(map[string]es.IndexHealth{"sts_topology": {NumberOfShards: 4, ActivePrimaryShards: 2}}),
		}}
		statusFn := newRestoreStatusFn(client, log, expected, testDatastreamPrefix, time.Nanosecond, 5)

		_, _, err := statusFn()
		require.NoError(t, err)

		_, isComplete, err := statusFn()
		require.Error(t, err)
		assert.False(t, isComplete)
		assert.ErrorContains(t, err, "stalled")
	})

	t.Run("zero waits indefinitely", func(t *testing.T) {
		client := &fakeHealthClient{results: []healthResult{
			ok(map[string]es.IndexHealth{"sts_topology": restored(1)}),
		}}
		statusFn := newRestoreStatusFn(client, log, expected, testDatastreamPrefix, 0, 5)

		for range 3 {
			status, isComplete, err := statusFn()
			require.NoError(t, err)
			assert.Equal(t, es.StatusInProgress, status)
			assert.False(t, isComplete)
		}
	})
}

func TestNewRestoreStatusFn_ILMDeletionRebasesHighWaterMark(t *testing.T) {
	expected := []string{"sts_topology", ".ds-sts_k8s_logs-2026.08.28-012011"}

	t.Run("later shard progress can exceed the rebased total", func(t *testing.T) {
		client := &fakeHealthClient{results: []healthResult{
			restoreHealth(3, true),
			restoreHealth(5, false),
			restoreHealth(6, false),
			restoreHealth(6, false),
		}}
		statusFn := newRestoreStatusFn(client, logger.New(true, false), expected, testDatastreamPrefix, time.Nanosecond, 5)

		for range 3 {
			status, isComplete, err := statusFn()
			require.NoError(t, err)
			assert.Equal(t, es.StatusInProgress, status)
			assert.False(t, isComplete)
		}

		_, isComplete, err := statusFn()
		require.Error(t, err)
		assert.False(t, isComplete)
		assert.ErrorContains(t, err, "stalled")
	})

	t.Run("the deletion itself does not reset the deadline", func(t *testing.T) {
		client := &fakeHealthClient{results: []healthResult{
			restoreHealth(3, true),
			restoreHealth(5, false),
			restoreHealth(5, false),
		}}
		statusFn := newRestoreStatusFn(client, logger.New(true, false), expected, testDatastreamPrefix, time.Nanosecond, 5)

		for range 2 {
			_, _, err := statusFn()
			require.NoError(t, err)
		}

		_, isComplete, err := statusFn()
		require.Error(t, err)
		assert.False(t, isComplete)
		assert.ErrorContains(t, err, "stalled")
	})
}

func restoreHealth(topologyPrimaries int, includeBackingIndex bool) healthResult {
	health := map[string]es.IndexHealth{
		"sts_topology": {NumberOfShards: 10, ActivePrimaryShards: topologyPrimaries},
	}
	if includeBackingIndex {
		health[".ds-sts_k8s_logs-2026.08.28-012011"] = restored(8)
	}

	return ok(health)
}

func TestNewRestoreStatusFn_TransientErrors(t *testing.T) {
	expected := statusFnExpected
	log := logger.New(true, false)

	t.Run("a dropped port-forward does not end the restore", func(t *testing.T) {
		client := &fakeHealthClient{results: []healthResult{
			{err: errors.New("connection refused")},
			{err: errors.New("connection refused")},
			ok(map[string]es.IndexHealth{"sts_topology": restored(1), "sts_events": restored(1)}),
		}}
		statusFn := newRestoreStatusFn(client, log, expected, testDatastreamPrefix, time.Minute, 5)

		for range 2 {
			status, isComplete, err := statusFn()
			require.NoError(t, err)
			assert.Equal(t, es.StatusInProgress, status)
			assert.False(t, isComplete)
		}

		status, isComplete, err := statusFn()
		require.NoError(t, err)
		assert.Equal(t, es.StatusSuccess, status)
		assert.True(t, isComplete)
	})

	t.Run("errors surface once they stop being transient", func(t *testing.T) {
		client := &fakeHealthClient{results: []healthResult{{err: errors.New("connection refused")}}}
		statusFn := newRestoreStatusFn(client, log, expected, testDatastreamPrefix, time.Minute, 3)

		for range 2 {
			_, _, err := statusFn()
			require.NoError(t, err)
		}

		_, _, err := statusFn()
		require.Error(t, err)
		assert.ErrorContains(t, err, "connection refused")
	})

	t.Run("a successful check clears earlier errors", func(t *testing.T) {
		client := &fakeHealthClient{results: []healthResult{
			{err: errors.New("blip")},
			ok(map[string]es.IndexHealth{"sts_topology": restored(1)}),
			{err: errors.New("blip")},
			{err: errors.New("blip")},
		}}
		statusFn := newRestoreStatusFn(client, log, expected, testDatastreamPrefix, time.Minute, 3)

		for range 4 {
			_, _, err := statusFn()
			require.NoError(t, err)
		}
	})
}

func TestReconnectingHealthClient(t *testing.T) {
	t.Run("passes through the caller's client while it works", func(t *testing.T) {
		health := map[string]es.IndexHealth{"sts_topology": restored(1)}
		seeded := &fakeHealthClient{results: []healthResult{ok(health)}}
		client := &reconnectingHealthClient{client: seeded}

		got, err := client.GetIndicesHealth()

		require.NoError(t, err)
		assert.Equal(t, health, got)
		assert.Equal(t, 1, seeded.calls)
	})

	t.Run("drops a broken client but never closes a port-forward it did not open", func(t *testing.T) {
		// The caller closes the port-forward it passed in, so adopting it here would double-close a
		// channel and panic. pf must stay nil until connect() opens one.
		seeded := &fakeHealthClient{results: []healthResult{{err: errors.New("connection refused")}}}
		client := &reconnectingHealthClient{client: seeded}

		_, err := client.GetIndicesHealth()

		require.Error(t, err)
		assert.Nil(t, client.client, "a broken client must be dropped so the next call reconnects")
		assert.Nil(t, client.pf, "must not adopt a port-forward it did not open")
	})

	t.Run("disconnect is safe to call repeatedly", func(t *testing.T) {
		client := &reconnectingHealthClient{client: &fakeHealthClient{}}

		assert.NotPanics(t, func() {
			client.disconnect()
			client.disconnect()
		})
	})
}
