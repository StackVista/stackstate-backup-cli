package restorelock

import (
	"testing"

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
			datastore: DatastoreElasticsearch,
			want:      "",
		},
		{
			name:      "clickhouse has no exclusion group",
			datastore: DatastoreClickhouse,
			want:      "",
		},
		{
			name:      "victoriametrics has no exclusion group",
			datastore: DatastoreVictoriaMetrics,
			want:      "",
		},
		{
			name:      "stackgraph has stackgraph group",
			datastore: DatastoreStackgraph,
			want:      ExclusionGroupStackgraph,
		},
		{
			name:      "settings has stackgraph group",
			datastore: DatastoreSettings,
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
			want:  []string{DatastoreStackgraph, DatastoreSettings},
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
			datastore1: DatastoreStackgraph,
			datastore2: DatastoreSettings,
			want:       true,
		},
		{
			name:       "settings and stackgraph are mutually exclusive (reversed)",
			datastore1: DatastoreSettings,
			datastore2: DatastoreStackgraph,
			want:       true,
		},
		{
			name:       "elasticsearch and clickhouse are not mutually exclusive",
			datastore1: DatastoreElasticsearch,
			datastore2: DatastoreClickhouse,
			want:       false,
		},
		{
			name:       "stackgraph and elasticsearch are not mutually exclusive",
			datastore1: DatastoreStackgraph,
			datastore2: DatastoreElasticsearch,
			want:       false,
		},
		{
			name:       "same datastore (stackgraph) is technically mutually exclusive with itself",
			datastore1: DatastoreStackgraph,
			datastore2: DatastoreStackgraph,
			want:       true,
		},
		{
			name:       "same datastore (elasticsearch) without group is not mutually exclusive",
			datastore1: DatastoreElasticsearch,
			datastore2: DatastoreElasticsearch,
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
