package restore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-zookeeper/zk"
	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/scale"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	zkSessionTimeout = 2 * time.Second
	hbaseSelector    = "app.kubernetes.io/name=hbase"
	// HDFS and HBase flush and hand off regions on shutdown, which outlasts the generic pod timeout.
	hbaseTerminationTimeout = 10 * time.Minute
	hbaseReadyTimeout       = 15 * time.Minute
	hbaseIDCheckInterval    = 5 * time.Second
	hbaseIDZnode            = "/hbase/hbaseid"
)

func deleteRecursive(zkConn *zk.Conn, path string, logger *logger.Logger) error {
	children, _, err := zkConn.Children(path)
	switch {
	case errors.Is(err, zk.ErrNoNode):
		return nil
	case err != nil:
		logger.Warningf("Unable to list znode for %s: %v", path, err)
	default:
		for _, child := range children {
			if err := deleteRecursive(zkConn, path+"/"+child, logger); err != nil {
				return err
			}
		}
	}
	if err := zkConn.Delete(path, -1); err != nil && !errors.Is(err, zk.ErrNoNode) {
		return err
	}
	return nil
}

func scaleDownHBase(ctx context.Context, k8sClient *k8s.Client, namespace string, logger *logger.Logger) error {
	logger.Println()
	logger.Infof("Scaling down HBase")
	if _, err := scale.ScaleDownWithTimeout(ctx, k8sClient, namespace, hbaseSelector, hbaseTerminationTimeout, logger); err != nil {
		return scaleUpHBaseAfterError(k8sClient, namespace, logger, err)
	}
	return nil
}

// WipePVCs deletes the HBase, HDFS and Tephra data and waits until HBase is serving again.
// An interrupt stops the waits so the callers' deferred scale-up and lock release still run.
func WipePVCs(ctx context.Context, k8sClient *k8s.Client, namespace, zkQuorum string, logger *logger.Logger) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !strings.Contains(zkQuorum, "-stackgraph") {
		zkConn, closeZK, err := connectZookeeper(k8sClient, namespace, zkQuorum, logger)
		if err != nil {
			return err
		}
		defer closeZK()

		if err = scaleDownHBase(ctx, k8sClient, namespace, logger); err != nil {
			return err
		}

		logger.Println()
		logger.Infof("Removing HBase and Tephra nodes from Zookeeper")

		if err := deleteRecursive(zkConn, "/hbase", logger); err != nil {
			return scaleUpHBaseAfterError(k8sClient, namespace, logger, err)
		}
		if err := deleteRecursive(zkConn, "/tx.service", logger); err != nil {
			return scaleUpHBaseAfterError(k8sClient, namespace, logger, err)
		}

		logger.Successf("Removed nodes from Zookeeper")
	} else {
		if err := scaleDownHBase(ctx, k8sClient, namespace, logger); err != nil {
			return err
		}
	}

	pvcs, err := k8sClient.Clientset().CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: hbaseSelector,
	})
	if err != nil {
		return scaleUpHBaseAfterError(k8sClient, namespace, logger, err)
	}

	logger.Println()
	logger.Infof("Deleting PVCs")
	for _, pvc := range pvcs.Items {
		if err := k8sClient.DeletePVC(pvc.Namespace, pvc.Name); err != nil {
			return scaleUpHBaseAfterError(k8sClient, namespace, logger, err)
		}
		logger.Infof("  - %s", pvc.Name)
	}
	logger.Successf("Deleted PVCs")

	logger.Println()
	if err := scale.ScaleUpFromAnnotations(k8sClient, namespace, hbaseSelector, logger); err != nil {
		return fmt.Errorf("failed to scale up HBase after deleting PVCs: %w", err)
	}
	return waitForHBase(ctx, k8sClient, namespace, zkQuorum, logger)
}

// waitForHBase blocks until HBase can serve the restore job, which otherwise fails with NoNode for /hbase/hbaseid.
// Pod readiness alone is not enough: the Mono readiness probe only checks the master info page.
func waitForHBase(ctx context.Context, k8sClient *k8s.Client, namespace, zkQuorum string, logger *logger.Logger) error {
	ctx, cancel := context.WithTimeout(ctx, hbaseReadyTimeout)
	defer cancel()

	if err := scale.WaitForReady(ctx, k8sClient, namespace, hbaseSelector, logger); err != nil {
		return fmt.Errorf("HBase did not become ready after deleting PVCs: %w", err)
	}

	// Mono runs Zookeeper inside the restarted pod, so connect only after it is ready.
	zkConn, closeZK, err := connectZookeeper(k8sClient, namespace, zkQuorum, logger)
	if err != nil {
		return err
	}
	defer closeZK()

	logger.Infof("Waiting for HBase to register %s in Zookeeper...", hbaseIDZnode)
	ticker := time.NewTicker(hbaseIDCheckInterval)
	defer ticker.Stop()
	for {
		exists, _, err := zkConn.Exists(hbaseIDZnode)
		if err != nil {
			logger.Warningf("Unable to check %s: %v", hbaseIDZnode, err)
		} else if exists {
			logger.Successf("HBase is ready")
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("HBase did not register %s after deleting PVCs: %w", hbaseIDZnode, ctx.Err())
		case <-ticker.C:
		}
	}
}

func connectZookeeper(k8sClient *k8s.Client, namespace, zkQuorum string, logger *logger.Logger) (*zk.Conn, func(), error) {
	zkHost, zkPortStr, portSpecified := strings.Cut(zkQuorum, ":")
	port := 2181
	if portSpecified {
		if zkPort, err := strconv.Atoi(zkPortStr); err != nil {
			logger.Warningf("Unable to parse port %s, falling back to %d", zkPortStr, port)
		} else {
			port = zkPort
		}
	}
	pf, err := portforward.SetupPortForward(k8sClient, namespace, zkHost, port, logger)
	if err != nil {
		return nil, nil, err
	}

	zkConn, _, err := zk.Connect([]string{fmt.Sprintf("localhost:%d", pf.LocalPort)}, zkSessionTimeout)
	if err != nil {
		close(pf.StopChan)
		return nil, nil, err
	}
	return zkConn, func() {
		zkConn.Close()
		close(pf.StopChan)
	}, nil
}

func scaleUpHBaseAfterError(k8sClient *k8s.Client, namespace string, logger *logger.Logger, cause error) error {
	if err := scale.ScaleUpFromAnnotations(k8sClient, namespace, hbaseSelector, logger); err != nil {
		logger.Warningf("Failed to scale up HBase: %v", err)
	}
	return cause
}
