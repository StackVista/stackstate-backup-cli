package portforward

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
)

type portForwardResult struct {
	stopChan  chan struct{}
	localPort int
	err       error
}

type fakeClient struct {
	results []portForwardResult
	calls   int
}

func (f *fakeClient) PortForwardService(_, _ string, _ int) (chan struct{}, int, error) {
	result := f.results[f.calls]
	f.calls++
	return result.stopChan, result.localPort, result.err
}

func TestSetupPortForward_RetriesTransientFailure(t *testing.T) {
	stopChan := make(chan struct{})
	client := &fakeClient{
		results: []portForwardResult{
			{err: errors.New("connection reset by peer")},
			{err: errors.New("error upgrading connection")},
			{stopChan: stopChan, localPort: 43210},
		},
	}
	log := logger.New(true, false)

	result, err := setupPortForward(client, "default", "test-service", 9200, log, 3, 0)

	require.NoError(t, err)
	assert.Equal(t, 3, client.calls)
	assert.Equal(t, stopChan, result.StopChan)
	assert.Equal(t, 43210, result.LocalPort)
}

func TestSetupPortForward_ReturnsLastErrorAfterRetries(t *testing.T) {
	client := &fakeClient{
		results: []portForwardResult{
			{err: errors.New("first failure")},
			{err: errors.New("second failure")},
			{err: errors.New("last failure")},
		},
	}
	log := logger.New(true, false)

	result, err := setupPortForward(client, "default", "test-service", 9200, log, 3, 0)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, 3, client.calls)
	assert.ErrorContains(t, err, "failed to setup port-forward after 3 attempts")
	assert.ErrorContains(t, err, "last failure")
}

func TestConn_Structure(t *testing.T) {
	stopChan := make(chan struct{})
	localPort := 8080

	result := &Conn{
		StopChan:  stopChan,
		LocalPort: localPort,
	}

	if result.StopChan == nil {
		t.Error("expected StopChan to be set")
	}
	if result.LocalPort != localPort {
		t.Errorf("expected LocalPort to be %d, got %d", localPort, result.LocalPort)
	}
}

func TestConn_ChannelCleanup(t *testing.T) {
	stopChan := make(chan struct{})

	result := &Conn{
		StopChan:  stopChan,
		LocalPort: 8080,
	}

	close(result.StopChan)

	select {
	case <-result.StopChan:
		// Successfully received from closed channel
	default:
		t.Error("expected StopChan to be closed")
	}
}
