package replication

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

const (
	minZooKeeperVoters = 3
	zooKeeperLeader    = "leader"
	zooKeeperFollower  = "follower"
)

const zookeeperQuery = `exec 3<>/dev/tcp/127.0.0.1/2181
printf mntr >&3
while true; do
  if IFS= read -r -t 5 -u 3 line; then
    printf '%s\n' "$line"
  else
    status=$?
    if [ "$status" -eq 1 ] && [ -z "$line" ]; then
      break
    fi
    exit 1
  fi
done`

type zooKeeperObservation struct {
	pod       string
	role      string
	peerState string
	voters    int
	synced    int
}

func (c *Checker) checkZooKeeper(ctx context.Context, inventory inventory) Result {
	members, err := inventory.members("zookeeper", "zookeeper", "zookeeper")
	if err != nil {
		return result("zookeeper", Unknown, err.Error())
	}
	if len(members) < minZooKeeperVoters {
		return result("zookeeper", Degraded, "at least three voting members are required for fault-tolerant ZooKeeper")
	}
	var observations []zooKeeperObservation
	for _, member := range members {
		if member.replicas != len(members) {
			return result("zookeeper", Unknown, "expected one chart-managed ZooKeeper ensemble")
		}
		observation, err := c.queryZooKeeper(ctx, member.pod.Name)
		if err != nil {
			return result("zookeeper", Unknown, err.Error())
		}
		observations = append(observations, observation)
	}
	report := evaluateZooKeeper(observations)
	if report.Status != Healthy {
		return report
	}
	// Recheck the leader after sampling followers; elections do not change Pod status.
	for n, observation := range observations {
		if observation.role != zooKeeperLeader {
			continue
		}
		current, err := c.queryZooKeeper(ctx, observation.pod)
		if err != nil {
			return result("zookeeper", Unknown, err.Error())
		}
		if current.role != zooKeeperLeader {
			return result("zookeeper", Unknown, "ZooKeeper leadership changed during the checks; repeat the observation")
		}
		observations[n] = current
	}
	return evaluateZooKeeper(observations)
}

func (c *Checker) queryZooKeeper(ctx context.Context, pod string) (zooKeeperObservation, error) {
	data, err := c.query(ctx, pod, "zookeeper", []string{"bash", "-ec", zookeeperQuery})
	if err != nil {
		return zooKeeperObservation{}, err
	}
	observation, err := parseZooKeeper(string(data), pod)
	if err != nil {
		return zooKeeperObservation{}, fmt.Errorf("%s: %w", pod, err)
	}
	return observation, nil
}

func parseZooKeeper(data, pod string) (zooKeeperObservation, error) {
	fields := make(map[string]string)
	for _, line := range strings.Split(data, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "\t")
		_, duplicate := fields[key]
		if !ok || key == "" || duplicate {
			return zooKeeperObservation{}, fmt.Errorf("invalid or unavailable ZooKeeper mntr response")
		}
		fields[key] = strings.TrimSpace(value)
	}
	if fields["zk_server_state"] == "" {
		return zooKeeperObservation{}, fmt.Errorf("missing zk_server_state in mntr response")
	}
	observation := zooKeeperObservation{pod: pod, role: fields["zk_server_state"], peerState: fields["zk_peer_state"]}
	voters, err := zooKeeperNumber(fields, "zk_quorum_size")
	if err != nil {
		return zooKeeperObservation{}, err
	}
	observation.voters = voters
	if observation.role == zooKeeperLeader {
		observation.synced, err = zooKeeperNumber(fields, "zk_synced_followers")
		if err != nil {
			return zooKeeperObservation{}, err
		}
	}
	return observation, nil
}

func zooKeeperNumber(fields map[string]string, key string) (int, error) {
	value, err := strconv.Atoi(fields[key])
	if err != nil || value < 0 {
		return 0, fmt.Errorf("missing or invalid %s in mntr response", key)
	}
	return value, nil
}

func evaluateZooKeeper(observations []zooKeeperObservation) Result {
	if len(observations) < minZooKeeperVoters {
		return result("zookeeper", Degraded, "at least three voting members are required for fault-tolerant ZooKeeper")
	}
	leaders := 0
	seen := make(map[string]bool)
	var problems []string
	for _, observation := range observations {
		if observation.pod == "" || seen[observation.pod] {
			return result("zookeeper", Unknown, "missing or duplicate ZooKeeper member evidence")
		}
		seen[observation.pod] = true
		if observation.voters != len(observations) {
			return result("zookeeper", Unknown, fmt.Sprintf("%s reports %d configured voters; %d members discovered", observation.pod, observation.voters, len(observations)))
		}
		switch observation.role {
		case zooKeeperLeader:
			leaders++
			if observation.synced != len(observations)-1 {
				problems = append(problems, fmt.Sprintf("%s reports %d/%d synchronized followers", observation.pod, observation.synced, len(observations)-1))
			}
		case zooKeeperFollower:
		default:
			problems = append(problems, fmt.Sprintf("%s is %s; expected a voting leader or follower", observation.pod, observation.role))
		}
		expectedPeerState := "following - broadcast"
		if observation.role == zooKeeperLeader {
			expectedPeerState = "leading - broadcast"
		}
		if observation.peerState != "" && observation.peerState != expectedPeerState {
			problems = append(problems, fmt.Sprintf("%s is not in broadcast state: %s", observation.pod, observation.peerState))
		}
	}
	if leaders != 1 {
		problems = append(problems, fmt.Sprintf("expected one ZooKeeper leader; observed %d", leaders))
	}
	if len(problems) > 0 {
		return Result{Component: "zookeeper", Status: Degraded, Messages: problems}
	}
	return result("zookeeper", Healthy, fmt.Sprintf("%d voting members available; one leader and %d synchronized followers", len(observations), len(observations)-1))
}
