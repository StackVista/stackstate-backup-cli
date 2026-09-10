package k8s

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

const maxExecOutput = 8 << 20

type boundedBuffer struct {
	buffer bytes.Buffer
	err    error
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > maxExecOutput-b.buffer.Len() {
		b.err = fmt.Errorf("query output exceeds %d bytes", maxExecOutput)
		return 0, b.err
	}
	return b.buffer.Write(p)
}

// Exec runs a command without a TTY, bounded by the caller's context and output limit.
func (c *Client) Exec(ctx context.Context, namespace, pod, container string, command []string) ([]byte, error) {
	request := c.clientset.CoreV1().RESTClient().Post().
		Namespace(namespace).Resource("pods").Name(pod).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(c.restConfig, http.MethodPost, request.URL())
	if err != nil {
		return nil, fmt.Errorf("create pod executor: %w", err)
	}
	var output boundedBuffer
	// Database tools can echo authentication details in stderr.
	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &output, Stderr: io.Discard}); err != nil {
		return nil, fmt.Errorf("query pod %s/%s: %w", namespace, pod, err)
	}
	if output.err != nil {
		return nil, output.err
	}
	return output.buffer.Bytes(), nil
}
