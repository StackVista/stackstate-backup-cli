package restore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
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

func scaleDownHBase(k8sClient *k8s.Client, namespace string, logger *logger.Logger) error {
	logger.Println()
	logger.Infof("Scaling down HBase")
	_, err := scale.ScaleDownWithTimeout(k8sClient, namespace, hbaseSelector, hbaseTerminationTimeout, logger)
	return err
}

func WipePVCs(ctx context.Context, k8sClient *k8s.Client, namespace, zkQuorum string, logger *logger.Logger) error {
	if !strings.Contains(zkQuorum, "-stackgraph") {
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
			return err
		}
		defer close(pf.StopChan)

		zkEndpoint := fmt.Sprintf("localhost:%d", pf.LocalPort)
		zkConn, _, err := zk.Connect(
			[]string{zkEndpoint},
			zkSessionTimeout,
		)
		if err != nil {
			return err
		}
		defer zkConn.Close()

		if err = scaleDownHBase(k8sClient, namespace, logger); err != nil {
			return err
		}

		logger.Println()
		logger.Infof("Removing HBase and Tephra nodes from Zookeeper")

		if err := deleteRecursive(zkConn, "/hbase", logger); err != nil {
			return err
		}
		if err := deleteRecursive(zkConn, "/tx.service", logger); err != nil {
			return err
		}

		logger.Successf("Removed nodes from Zookeeper")
	} else {
		if err := scaleDownHBase(k8sClient, namespace, logger); err != nil {
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
	// The restore job needs live region servers; starting it during HBase initialization fails with NoNode for /hbase/hbaseid.
	if err := scale.WaitForReady(k8sClient, namespace, hbaseSelector, hbaseReadyTimeout, logger); err != nil {
		return fmt.Errorf("HBase did not become ready after deleting PVCs: %w", err)
	}

	return nil
}

func scaleUpHBaseAfterError(k8sClient *k8s.Client, namespace string, logger *logger.Logger, cause error) error {
	if err := scale.ScaleUpFromAnnotations(k8sClient, namespace, hbaseSelector, logger); err != nil {
		logger.Warningf("Failed to scale up HBase: %v", err)
	}
	return cause
}
