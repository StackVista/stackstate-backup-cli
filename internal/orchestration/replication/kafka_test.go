package replication

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKafkaQueryDoesNotInheritBrokerJMXPort(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is required to exercise the in-pod query")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
test -z "${JMX_PORT:-}" || exit 1
test -z "${KAFKA_JMX_OPTS:-}" || exit 1
printf '%s\n' "$KAFKA_OPTS" "$@"
`
	path := filepath.Join(dir, "kafka-topics.sh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o600))
	require.NoError(t, os.Chmod(path, 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("JMX_PORT", "5555")
	t.Setenv("KAFKA_JMX_OPTS", "-Dcom.sun.management.jmxremote.port=5555")
	t.Setenv("KAFKA_OPTS", "-Djava.security.auth.login.config=/mounted/jaas.conf")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, bash, "-ec", kafkaQuery, "replication-check",
		"--bootstrap-server", "localhost:9092", "--describe", "--command-config", "/mounted/client properties")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	assert.Equal(t, "-Djava.security.auth.login.config=/mounted/jaas.conf\n--bootstrap-server\nlocalhost:9092\n--describe\n--command-config\n/mounted/client properties\n", string(output))
}
