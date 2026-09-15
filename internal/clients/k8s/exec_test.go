package k8s

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecOutputLimit(t *testing.T) {
	var output boundedBuffer
	n, err := output.Write([]byte("report"))
	require.NoError(t, err)
	assert.Equal(t, 6, n)
	_, err = io.Copy(&output, io.LimitReader(strings.NewReader(strings.Repeat("x", maxExecOutput)), int64(maxExecOutput)))
	require.ErrorContains(t, err, "query output exceeds")
	assert.LessOrEqual(t, output.buffer.Len(), maxExecOutput)
}
