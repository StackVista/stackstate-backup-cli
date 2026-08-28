package portforward

import (
	"fmt"
	"time"

	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
)

const (
	portForwardMaxAttempts = 3
	portForwardRetryDelay  = 2 * time.Second
)

type portForwardClient interface {
	PortForwardService(namespace, serviceName string, remotePort int) (chan struct{}, int, error)
}

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
	k8sClient portForwardClient,
	namespace string,
	serviceName string,
	remotePort int,
	log *logger.Logger,
) (*Conn, error) {
	return setupPortForward(
		k8sClient,
		namespace,
		serviceName,
		remotePort,
		log,
		portForwardMaxAttempts,
		portForwardRetryDelay,
	)
}

func setupPortForward(
	k8sClient portForwardClient,
	namespace string,
	serviceName string,
	remotePort int,
	log *logger.Logger,
	maxAttempts int,
	retryDelay time.Duration,
) (*Conn, error) {
	log.Infof("Setting up port-forward to %s:%d in namespace %s...", serviceName, remotePort, namespace)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		stopChan, actualLocalPort, err := k8sClient.PortForwardService(namespace, serviceName, remotePort)
		if err == nil {
			log.Successf("Port-forward established on localhost:%d", actualLocalPort)

			return &Conn{
				StopChan:  stopChan,
				LocalPort: actualLocalPort,
			}, nil
		}

		if attempt == maxAttempts {
			return nil, fmt.Errorf("failed to setup port-forward after %d attempts: %w", attempt, err)
		}

		log.Warningf(
			"Port-forward attempt %d/%d failed: %v; retrying in %s",
			attempt,
			maxAttempts,
			err,
			retryDelay,
		)
		time.Sleep(retryDelay)
	}

	return nil, fmt.Errorf("failed to setup port-forward")
}
