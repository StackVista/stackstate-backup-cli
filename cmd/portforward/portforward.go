package portforward

import (
	"fmt"

	"github.com/stackvista/stackstate-backup-cli/internal/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/logger"
)

// Conn contains the channels needed to manage a port-forward connection
type Conn struct {
	StopChan  chan struct{}
	ReadyChan <-chan struct{}
	LocalPort int
}

// SetupPortForward establishes a port-forward to a Kubernetes service and waits for it to be ready.
// It returns a Conn containing the stop and ready channels, plus the local port.
// The caller is responsible for closing the StopChan when done.
func SetupPortForward(
	k8sClient *k8s.Client,
	namespace string,
	serviceName string,
	localPort int,
	remotePort int,
	log *logger.Logger,
) (*Conn, error) {
	log.Infof("Setting up port-forward to %s:%d in namespace %s...", serviceName, remotePort, namespace)

	stopChan, readyChan, err := k8sClient.PortForwardService(namespace, serviceName, localPort, remotePort)
	if err != nil {
		return nil, fmt.Errorf("failed to setup port-forward: %w", err)
	}

	// Wait for port-forward to be ready
	<-readyChan

	log.Successf("Port-forward established successfully")

	return &Conn{
		StopChan:  stopChan,
		ReadyChan: readyChan,
		LocalPort: localPort,
	}, nil
}
