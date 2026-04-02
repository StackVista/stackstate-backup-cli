package elasticsearch

import (
	"testing"

	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescribeCmd_Unit(t *testing.T) {
	flags := config.NewCLIGlobalFlags()
	flags.Namespace = testNamespace
	flags.ConfigMapName = testConfigMapName
	cmd := describeCmd(flags)

	assert.Equal(t, "describe", cmd.Use)
	assert.Equal(t, "Show detailed information about an Elasticsearch snapshot", cmd.Short)
	assert.NotNil(t, cmd.Run)

	snapshotFlag := cmd.Flags().Lookup("snapshot")
	require.NotNil(t, snapshotFlag)
	assert.Equal(t, "s", snapshotFlag.Shorthand)

	// Verify snapshot flag is required
	annotations := snapshotFlag.Annotations
	require.Contains(t, annotations, "cobra_annotation_bash_completion_one_required_flag")
}
