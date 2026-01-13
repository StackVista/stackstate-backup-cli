// Package restorelock provides mechanisms to prevent parallel restore operations
// for the same datastore or mutually exclusive datastores.
package restorelock

// Datastore identifiers used for restore lock tracking
const (
	DatastoreElasticsearch   = "elasticsearch"
	DatastoreClickhouse      = "clickhouse"
	DatastoreVictoriaMetrics = "victoriametrics"
	DatastoreStackgraph      = "stackgraph"
	DatastoreSettings        = "settings"
)

// MutualExclusionGroup identifies a group of datastores that cannot be restored concurrently.
// Datastores in the same group share underlying data or have dependencies that make
// parallel restores unsafe.
const (
	// ExclusionGroupStackgraph groups Stackgraph and Settings restores
	// because Settings restore modifies Stackgraph/HBase data
	ExclusionGroupStackgraph = "stackgraph"
)

// datastoreMutualExclusion maps each datastore to its mutual exclusion group.
// Empty string means no mutual exclusion (datastore is independent).
var datastoreMutualExclusion = map[string]string{
	DatastoreElasticsearch:   "", // Independent
	DatastoreClickhouse:      "", // Independent
	DatastoreVictoriaMetrics: "", // Independent
	DatastoreStackgraph:      ExclusionGroupStackgraph,
	DatastoreSettings:        ExclusionGroupStackgraph,
}

// GetMutualExclusionGroup returns the mutual exclusion group for a datastore.
// Returns empty string if the datastore has no mutual exclusion constraints.
func GetMutualExclusionGroup(datastore string) string {
	return datastoreMutualExclusion[datastore]
}

// GetDatastoresInGroup returns all datastores that belong to the given mutual exclusion group.
// Returns nil if the group doesn't exist or is empty.
func GetDatastoresInGroup(group string) []string {
	if group == "" {
		return nil
	}

	var datastores []string
	for ds, g := range datastoreMutualExclusion {
		if g == group {
			datastores = append(datastores, ds)
		}
	}
	return datastores
}

// AreDatastoresMutuallyExclusive checks if two datastores are in the same mutual exclusion group.
func AreDatastoresMutuallyExclusive(datastore1, datastore2 string) bool {
	group1 := GetMutualExclusionGroup(datastore1)
	group2 := GetMutualExclusionGroup(datastore2)

	// Both must have a non-empty group and the groups must match
	return group1 != "" && group1 == group2
}
