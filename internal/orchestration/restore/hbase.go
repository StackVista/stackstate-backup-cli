package restore

import (
	"context"
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

const zkSessionTimeout = 2 * time.Second

func deleteRecursive(zkConn *zk.Conn, path string, logger *logger.Logger) {
	children, _, err := zkConn.Children(path)
	if err != nil {
		logger.Warningf("Unable to list znode for %s: %v", path, err)
	} else {
		for _, child := range children {
			deleteRecursive(zkConn, path+"/"+child, logger)
		}
	}
	err = zkConn.Delete(path, -1)
	if err != nil {
		logger.Warningf("Failed to delete znode %s: %v", path, err)
	}
}

func scaleDownHBase(k8sClient *k8s.Client, namespace string, logger *logger.Logger) error {
	logger.Println()
	logger.Infof("Scaling down HBase")
	_, err := scale.ScaleDown(k8sClient, namespace, "app.kubernetes.io/name=hbase", logger)
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

		deleteRecursive(zkConn, "/hbase", logger)
		deleteRecursive(zkConn, "/tx.service", logger)

		logger.Successf("Removed nodes from Zookeeper")
	} else {
		if err := scaleDownHBase(k8sClient, namespace, logger); err != nil {
			return err
		}
	}

	defer func() {
		err := scale.ScaleUpFromAnnotations(k8sClient, namespace, "app.kubernetes.io/name=hbase", logger)
		if err != nil {
			logger.Warningf("Failed to scale up HBase: %v", err)
		}
	}()

	// List pvcs in the hbase application
	pvcs, err := k8sClient.Clientset().CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=hbase",
	})
	if err != nil {
		return err
	}

	logger.Println()
	logger.Infof("Deleting PVCs")
	for _, pvc := range pvcs.Items {
		if err := k8sClient.DeletePVC(pvc.Namespace, pvc.Name); err != nil {
			return err
		} else {
			logger.Infof("  - %s", pvc.Name)
		}
	}
	logger.Successf("Deleted PVCs")

	logger.Println()
	logger.Infof("Scaled up HBase after deleting PVCs")

	return nil
}
