package k8s

import (
	"testing"

	"github.com/go-openapi/swag/jsonutils"
	"github.com/go-openapi/swag/yamlutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestSwagOrderedJSONCompatibility(t *testing.T) {
	input := []byte(`{"z":null,"a":{"yes":"yes","enabled":true,"number":42},"items":[1,"on",false]}`)
	var ordered jsonutils.JSONMapSlice
	require.NoError(t, jsonutils.ReadJSON(input, &ordered))
	output, err := jsonutils.WriteJSON(ordered)
	require.NoError(t, err)
	assert.Equal(t, string(input), string(output))
}

func TestSwagYAMLNodeCompatibility(t *testing.T) {
	input := []byte("z: &value\n  text: |-\n    first\n    second\n  legacy: yes\n  enabled: true\n  number: 42\na: *value\nempty: null\n")
	const expected = `{"z":{"text":"first\nsecond","legacy":"yes","enabled":true,"number":42},"a":{"text":"first\nsecond","legacy":"yes","enabled":true,"number":42},"empty":null}`
	var node yaml.Node
	require.NoError(t, yaml.Unmarshal(input, &node))
	document, err := yamlutils.BytesToYAMLDoc(input)
	require.NoError(t, err)
	for _, value := range []any{&node, document} {
		output, err := yamlutils.YAMLToJSON(value)
		require.NoError(t, err)
		assert.Equal(t, expected, string(output))
	}
}
