package elasticsearch

// Interface defines the contract for Elasticsearch client operations
// This interface allows for easy mocking in tests
type Interface interface {
	// Snapshot operations
	ListSnapshots(repository string) ([]Snapshot, error)
	GetSnapshot(repository, snapshotName string) (*Snapshot, error)
	RestoreSnapshot(repository, snapshotName, indicesPattern string, partial bool) error
	GetRestoreStatus(repository, snapshotName string) (string, bool, error)

	// Index operations
	ListIndices(pattern string) ([]string, error)
	ListIndicesDetailed() ([]IndexInfo, error)
	DeleteIndex(index string) error
	IndexExists(index string) (bool, error)

	// Datastream operations
	RolloverDatastream(datastreamName string) error

	// Repository and SLM operations
	DeleteSnapshotRepository(name string) error
	ConfigureSnapshotRepository(name, bucket, endpoint, basePath, accessKey, secretKey string) error
	ConfigureSLMPolicy(name, schedule, snapshotName, repository, indices, expireAfter string, minCount, maxCount int) error
}

// Ensure *Client implements Interface
var _ Interface = (*Client)(nil)
