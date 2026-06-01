package utils

import (
	"encoding/json"
	"strings"
)

func ParseUpdates(updateString string) map[string]interface{} {
	updates := make(map[string]interface{})

	// Split the input string into key-value pairs
	pairs := strings.Split(updateString, " ")

	for _, pair := range pairs {
		// Split each pair into key and value
		parts := strings.Split(pair, ":")
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])

			// Add key-value pair to the map
			updates[key] = strings.ReplaceAll(value, `"`, "")
		}
	}

	return updates
}

// ABOUT: This method is used to extract Fields and Values from JSON Byte
func ExtractFieldsAndValues(data []byte) ([]string, []interface{}, error) {
	var result map[string]interface{}

	// Unmarshal JSON data into a map
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, nil, err
	}

	// Extract field names and values
	var fields []string
	var values []interface{}

	for key, value := range result {
		fields = append(fields, key)
		values = append(values, value)
	}

	return fields, values, nil
}
