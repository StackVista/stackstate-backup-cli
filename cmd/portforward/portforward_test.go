package portforward

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/stackvista/stackstate-backup-cli/internal/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/logger"
)

func TestSetupPortForward_ServiceNotFound(t *testing.T) {
	fakeClientset := fake.NewSimpleClientset()
	client := k8s.NewTestClient(fakeClientset)
	log := logger.New(true, false)

	_, err := SetupPortForward(client, "default", "nonexistent-service", 8080, 9200, log)
	if err == nil {
		t.Fatal("expected error for nonexistent service, got nil")
	}
}

func TestSetupPortForward_NoPodsFound(t *testing.T) {
	fakeClientset := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-service",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app": "test",
				},
			},
		},
	)
	client := k8s.NewTestClient(fakeClientset)
	log := logger.New(true, false)

	_, err := SetupPortForward(client, "default", "test-service", 8080, 9200, log)
	if err == nil {
		t.Fatal("expected error for service with no pods, got nil")
	}
}

func TestSetupPortForward_NoRunningPods(t *testing.T) {
	fakeClientset := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-service",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app": "test",
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
				Labels: map[string]string{
					"app": "test",
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
			},
		},
	)
	client := k8s.NewTestClient(fakeClientset)
	log := logger.New(true, false)

	_, err := SetupPortForward(client, "default", "test-service", 8080, 9200, log)
	if err == nil {
		t.Fatal("expected error for service with no running pods, got nil")
	}
}

func TestConn_Structure(t *testing.T) {
	stopChan := make(chan struct{})
	readyChan := make(chan struct{})
	localPort := 8080

	result := &Conn{
		StopChan:  stopChan,
		ReadyChan: readyChan,
		LocalPort: localPort,
	}

	if result.StopChan == nil {
		t.Error("expected StopChan to be set")
	}
	if result.ReadyChan == nil {
		t.Error("expected ReadyChan to be set")
	}
	if result.LocalPort != localPort {
		t.Errorf("expected LocalPort to be %d, got %d", localPort, result.LocalPort)
	}
}

func TestConn_ChannelCleanup(t *testing.T) {
	stopChan := make(chan struct{})
	readyChan := make(chan struct{})

	result := &Conn{
		StopChan:  stopChan,
		ReadyChan: readyChan,
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
