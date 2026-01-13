package restorelock

import (
	"testing"

	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stretchr/testify/assert"
)

func TestGetMutualExclusionGroup(t *testing.T) {
	tests := []struct {
		name      string
		datastore string
		want      string
	}{
		{
			name:      "elasticsearch has no exclusion group",
			datastore: config.DatastoreElasticsearch,
			want:      "",
		},
		{
			name:      "clickhouse has no exclusion group",
			datastore: config.DatastoreClickhouse,
			want:      "",
		},
		{
			name:      "victoriametrics has no exclusion group",
			datastore: config.DatastoreVictoriaMetrics,
			want:      "",
		},
		{
			name:      "stackgraph has stackgraph group",
			datastore: config.DatastoreStackgraph,
			want:      ExclusionGroupStackgraph,
		},
		{
			name:      "settings has stackgraph group",
			datastore: config.DatastoreSettings,
			want:      ExclusionGroupStackgraph,
		},
		{
			name:      "unknown datastore returns empty string",
			datastore: "unknown",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetMutualExclusionGroup(tt.datastore)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetDatastoresInGroup(t *testing.T) {
	tests := []struct {
		name  string
		group string
		want  []string
	}{
		{
			name:  "empty group returns nil",
			group: "",
			want:  nil,
		},
		{
			name:  "stackgraph group contains stackgraph and settings",
			group: ExclusionGroupStackgraph,
			want:  []string{config.DatastoreStackgraph, config.DatastoreSettings},
		},
		{
			name:  "unknown group returns nil",
			group: "unknown-group",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetDatastoresInGroup(tt.group)
			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				assert.ElementsMatch(t, tt.want, got)
			}
		})
	}
}

func TestAreDatastoresMutuallyExclusive(t *testing.T) {
	tests := []struct {
		name       string
		datastore1 string
		datastore2 string
		want       bool
	}{
		{
			name:       "stackgraph and settings are mutually exclusive",
			datastore1: config.DatastoreStackgraph,
			datastore2: config.DatastoreSettings,
			want:       true,
		},
		{
			name:       "settings and stackgraph are mutually exclusive (reversed)",
			datastore1: config.DatastoreSettings,
			datastore2: config.DatastoreStackgraph,
			want:       true,
		},
		{
			name:       "elasticsearch and clickhouse are not mutually exclusive",
			datastore1: config.DatastoreElasticsearch,
			datastore2: config.DatastoreClickhouse,
			want:       false,
		},
		{
			name:       "stackgraph and elasticsearch are not mutually exclusive",
			datastore1: config.DatastoreStackgraph,
			datastore2: config.DatastoreElasticsearch,
			want:       false,
		},
		{
			name:       "same datastore (stackgraph) is technically mutually exclusive with itself",
			datastore1: config.DatastoreStackgraph,
			datastore2: config.DatastoreStackgraph,
			want:       true,
		},
		{
			name:       "same datastore (elasticsearch) without group is not mutually exclusive",
			datastore1: config.DatastoreElasticsearch,
			datastore2: config.DatastoreElasticsearch,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AreDatastoresMutuallyExclusive(tt.datastore1, tt.datastore2)
			assert.Equal(t, tt.want, got)
		})
	}
}
