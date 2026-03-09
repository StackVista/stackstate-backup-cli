package portforward

import (
	"fmt"

	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
)

// Conn contains the channels needed to manage a port-forward connection
type Conn struct {
	StopChan  chan struct{}
	LocalPort int
}

// SetupPortForward establishes a port-forward to a Kubernetes service and waits for it to be ready.
// It uses OS dynamic port allocation so the local port is determined automatically.
// It returns a Conn containing the stop channel and the actual local port.
// The caller is responsible for closing the StopChan when done.
func SetupPortForward(
	k8sClient *k8s.Client,
	namespace string,
	serviceName string,
	remotePort int,
	log *logger.Logger,
) (*Conn, error) {
	log.Infof("Setting up port-forward to %s:%d in namespace %s...", serviceName, remotePort, namespace)

	stopChan, actualLocalPort, err := k8sClient.PortForwardService(namespace, serviceName, remotePort)
	if err != nil {
		return nil, fmt.Errorf("failed to setup port-forward: %w", err)
	}

	log.Successf("Port-forward established on localhost:%d", actualLocalPort)

	return &Conn{
		StopChan:  stopChan,
		LocalPort: actualLocalPort,
	}, nil
}
