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

func TestTransactionTopicQueryUsesDirectConfigEvidence(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is required to exercise the in-pod query")
	}
	dir := t.TempDir()
	// Model the 4.1 listing precheck separately from the broker's DescribeConfigs response.
	script := `#!/bin/sh
test -z "${JMX_PORT:-}" && test -z "${KAFKA_JMX_OPTS:-}" || exit 1
test "$1" = "--bootstrap-server" && test "$2" = "localhost:9092" || exit 1
test "$3" = "--command-config" && test "$4" = "/mounted/client properties" || exit 1
shift 4
if [ "$KAFKA_TEST_VERSION" = "4.1" ] && [ "$KAFKA_TEST_EXIT" != "0" ]; then
  case " $* " in
    *" --all "*) ;;
    *) printf "The topic '__transaction_state' doesn't exist and doesn't have dynamic config.\n"; exit 0 ;;
  esac
fi
printf '%s\n' "$KAFKA_TEST_OUTPUT"
exit "$KAFKA_TEST_EXIT"
`
	path := filepath.Join(dir, "kafka-configs.sh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o600))
	require.NoError(t, os.Chmod(path, 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("JMX_PORT", "5555")
	t.Setenv("KAFKA_JMX_OPTS", "-Dcom.sun.management.jmxremote.port=5555")
	tests := []struct {
		name, output, exit, expected string
	}{
		{"present", "All configs for topic __transaction_state are:", "0", "present"},
		{"absent", "org.apache.kafka.common.errors.UnknownTopicOrPartitionException: unknown topic", "1", "absent"},
		{"ACL hidden", "org.apache.kafka.common.errors.TopicAuthorizationException: denied", "1", "unverified"},
		{"authorization overrides absence", "TopicAuthorizationException UnknownTopicOrPartitionException", "1", "unverified"},
		{"timeout", "TimeoutException: broker unavailable", "1", "unverified"},
	}
	for _, version := range []string{"3.9", "4.1"} {
		for _, test := range tests {
			t.Run(version+"/"+test.name, func(t *testing.T) {
				t.Setenv("KAFKA_TEST_VERSION", version)
				t.Setenv("KAFKA_TEST_OUTPUT", test.output)
				t.Setenv("KAFKA_TEST_EXIT", test.exit)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, bash, "-ec", transactionTopicQuery, "replication-check",
					"--bootstrap-server", "localhost:9092", "--command-config", "/mounted/client properties")
				output, err := command.CombinedOutput()
				require.NoError(t, err, string(output))
				assert.Equal(t, test.expected+"\n", string(output))
			})
		}
	}
}
