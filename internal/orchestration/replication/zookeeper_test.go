package replication

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func zooKeeperFixture(role string) string {
	data := "zk_version\t3.9.5\nzk_server_state\t" + role + "\nzk_quorum_size\t3\nzk_avg_latency\t0.123\n"
	if role == zooKeeperLeader {
		return data + "zk_peer_state\tleading - broadcast\nzk_synced_followers\t2\n"
	}
	return data + "zk_peer_state\tfollowing - broadcast\nzk_synced_observers\tnull\n"
}

func TestZooKeeperParsing(t *testing.T) {
	tests := []struct {
		name, data string
		valid      bool
	}{
		{zooKeeperLeader, zooKeeperFixture(zooKeeperLeader), true},
		{zooKeeperFollower, zooKeeperFixture(zooKeeperFollower), true},
		{"optional peer state absent", strings.ReplaceAll(zooKeeperFixture(zooKeeperFollower), "zk_peer_state\tfollowing - broadcast\n", ""), true},
		{"disabled command", "mntr is not executed because it is not in the whitelist.\n", false},
		{"not serving", "This ZooKeeper instance is not currently serving requests\n", false},
		{"empty response", "", false},
		{"missing role", "zk_quorum_size\t3\n", false},
		{"missing membership", "zk_server_state\tfollower\n", false},
		{"missing synchronized count", "zk_server_state\tleader\nzk_quorum_size\t3\n", false},
		{"duplicate role", zooKeeperFixture(zooKeeperLeader) + "zk_server_state\tfollower\n", false},
		{"duplicate empty value", "zk_version\t\n" + zooKeeperFixture(zooKeeperLeader), false},
		{"negative synchronized count", strings.ReplaceAll(zooKeeperFixture(zooKeeperLeader), "zk_synced_followers\t2", "zk_synced_followers\t-1"), false},
		{"invalid membership", strings.ReplaceAll(zooKeeperFixture(zooKeeperFollower), "zk_quorum_size\t3", "zk_quorum_size\tnull"), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observation, err := parseZooKeeper(test.data, "zk-0")
			if !test.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 3, observation.voters)
			assert.Equal(t, "zk-0", observation.pod)
		})
	}
}

func TestZooKeeperRequiresFullVotingEnsemble(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]zooKeeperObservation) []zooKeeperObservation
		status string
	}{
		{"healthy", func(o []zooKeeperObservation) []zooKeeperObservation { return o }, Healthy},
		{"quorum without full recovery", func(o []zooKeeperObservation) []zooKeeperObservation { o[0].synced = 1; return o }, Degraded},
		{"no leader", func(o []zooKeeperObservation) []zooKeeperObservation { o[0].role = zooKeeperFollower; return o }, Degraded},
		{"two leaders", func(o []zooKeeperObservation) []zooKeeperObservation { o[1].role = zooKeeperLeader; return o }, Degraded},
		{"election", func(o []zooKeeperObservation) []zooKeeperObservation { o[1].role = "looking"; return o }, Degraded},
		{"observer", func(o []zooKeeperObservation) []zooKeeperObservation { o[1].role = "observer"; return o }, Degraded},
		{"standalone", func(o []zooKeeperObservation) []zooKeeperObservation { o[1].role = "standalone"; return o }, Degraded},
		{"synchronizing", func(o []zooKeeperObservation) []zooKeeperObservation {
			o[1].peerState = "following - synchronization"
			return o
		}, Degraded},
		{"inconsistent role", func(o []zooKeeperObservation) []zooKeeperObservation {
			o[0].peerState = "following - broadcast"
			return o
		}, Degraded},
		{"unexpected voter configuration", func(o []zooKeeperObservation) []zooKeeperObservation { o[1].voters = 5; return o }, Unknown},
		{"duplicate member", func(o []zooKeeperObservation) []zooKeeperObservation { o[1].pod = o[0].pod; return o }, Unknown},
		{"too few voters", func(o []zooKeeperObservation) []zooKeeperObservation { return o[:2] }, Degraded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observations := []zooKeeperObservation{
				{pod: "zk-0", role: zooKeeperLeader, voters: 3, synced: 2},
				{pod: "zk-1", role: zooKeeperFollower, voters: 3},
				{pod: "zk-2", role: zooKeeperFollower, voters: 3},
			}
			assert.Equal(t, test.status, evaluateZooKeeper(test.mutate(observations)).Status)
		})
	}
}

func TestZooKeeperQueriesEveryMemberAndRechecksLeader(t *testing.T) {
	tests := []struct {
		name, status string
		finalRole    string
		finalSynced  string
		queryError   bool
	}{
		{name: "healthy", status: Healthy, finalRole: zooKeeperLeader, finalSynced: "2"},
		{name: "election during observation", status: Unknown, finalRole: zooKeeperFollower},
		{name: "follower falls behind during observation", status: Degraded, finalRole: zooKeeperLeader, finalSynced: "1"},
		{name: "query denied", status: Unknown, queryError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var queried []string
			kube := &fakeKubernetes{
				client: fake.NewSimpleClientset(databaseObjects("zookeeper", "zookeeper", "zookeeper", 3)...),
				exec: func(_ context.Context, namespace, pod, container string, command []string) ([]byte, error) {
					assert.Equal(t, "test", namespace)
					assert.Equal(t, "zookeeper", container)
					assert.Equal(t, []string{"bash", "-ec", zookeeperQuery}, command)
					queried = append(queried, pod)
					if test.queryError {
						return nil, fmt.Errorf("query denied")
					}
					if len(queried) == 4 {
						return []byte(strings.ReplaceAll(zooKeeperFixture(test.finalRole), "zk_synced_followers\t2", "zk_synced_followers\t"+test.finalSynced)), nil
					}
					if pod == "zookeeper-0" {
						return []byte(zooKeeperFixture(zooKeeperLeader)), nil
					}
					return []byte(zooKeeperFixture(zooKeeperFollower)), nil
				},
			}
			options := testOptions()
			options.Components = []string{"zookeeper"}
			probe, err := New(kube, options)
			require.NoError(t, err)
			assert.Equal(t, test.status, probe.Check(context.Background()).Status)
			if !test.queryError {
				assert.Equal(t, []string{"zookeeper-0", "zookeeper-1", "zookeeper-2", "zookeeper-0"}, queried)
			}
		})
	}
}

func TestZooKeeperMissingPodCannotPass(t *testing.T) {
	objects := databaseObjects("zookeeper", "zookeeper", "zookeeper", 3)
	kube := &fakeKubernetes{client: fake.NewSimpleClientset(objects[:len(objects)-1]...)}
	options := testOptions()
	options.Components = []string{"zookeeper"}
	probe, err := New(kube, options)
	require.NoError(t, err)
	assert.Equal(t, Unknown, probe.Check(context.Background()).Status)
	assert.Zero(t, kube.calls)
}
