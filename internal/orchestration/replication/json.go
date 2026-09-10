package replication

import (
	"encoding/json"
	"fmt"
	"strconv"
)

func number(fields map[string]json.RawMessage, key string) (int64, error) {
	raw, found := fields[key]
	if !found || string(raw) == "null" {
		return 0, fmt.Errorf("missing numeric field %s", key)
	}
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil {
		var quoted string
		if err := json.Unmarshal(raw, &quoted); err != nil {
			return 0, fmt.Errorf("invalid integer field %s", key)
		}
		var parseErr error
		value, parseErr = strconv.ParseInt(quoted, 10, 64)
		if parseErr != nil {
			return 0, fmt.Errorf("invalid integer field %s", key)
		}
	}
	if value < 0 {
		return 0, fmt.Errorf("invalid nonnegative integer field %s", key)
	}
	return value, nil
}

func text(fields map[string]json.RawMessage, key string) (string, error) {
	raw, found := fields[key]
	if !found || string(raw) == "null" {
		return "", fmt.Errorf("missing string field %s", key)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("invalid string field %s", key)
	}
	return value, nil
}

func numbers(fields map[string]json.RawMessage, keys ...string) (map[string]int64, error) {
	values := make(map[string]int64, len(keys))
	for _, key := range keys {
		value, err := number(fields, key)
		if err != nil {
			return nil, err
		}
		values[key] = value
	}
	return values, nil
}
