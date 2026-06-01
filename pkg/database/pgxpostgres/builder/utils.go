package builder

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
)

// ABOUT: Helper function to convert a struct to []interface{}
func StructSlice(arg interface{}) ([]interface{}, error) {
	v := reflect.ValueOf(arg)
	if v.Kind() == reflect.Struct {
		numFields := v.NumField()
		result := make([]interface{}, 0)
		for i := 0; i < numFields; i++ {
			field := v.Field(i)
			zeroValue := reflect.Zero(field.Type())
			if (field.Kind() == reflect.Ptr && !field.IsNil()) || (field.Interface() != zeroValue.Interface()) {
				result = append(result, field.Interface())
			}
		}
		return result, nil
	}
	return nil, errors.New("error converting data to interface slice")
}

// ABOUT: Helper function to convert a slice to []interface{}
func InterfaceSlice(arg interface{}) ([]interface{}, error) {
	switch v := arg.(type) {
	case []interface{}:
		return v, nil
	default:
		return nil, nil
	}
}

// ABOUT: Method to get Table Name from the GO Struct
func GetTableName[T any](model T) (string, error) {
	modelType, err := GetStructType(model)
	if err != nil {
		return "", err
	}

	return SnakeCase(modelType.Name()), nil
}

// ABOUT: Method to get Field Names from the GO Struct
func GetFieldNames[T any](model T) ([]string, error) {
	modelType, err := GetStructType(model)
	if err != nil {
		return nil, err
	}

	var fieldNames []string
	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		tableFieldName := field.Tag.Get("db")
		if tableFieldName == "" {
			fieldName := SnakeCase(field.Name)
			fieldNames = append(fieldNames, fieldName)
		} else {
			fieldNames = append(fieldNames, tableFieldName)
		}
	}

	return fieldNames, nil
}

// ABOUT: Method to Get Struct Type
func GetStructType[T any](model T) (reflect.Type, error) {
	modelValue := reflect.ValueOf(model)
	if modelValue.Kind() == reflect.Ptr {
		modelValue = modelValue.Elem()
	}

	if modelValue.Kind() != reflect.Struct {
		return nil, errors.New("model must be a struct or a pointer to a struct")
	}

	return modelValue.Type(), nil
}

// ABOUT: This Method converts CamelCase to snake_case
func SnakeCase(camel string) string {
	var buf bytes.Buffer
	for _, c := range camel {
		if 'A' <= c && c <= 'Z' {
			// Convert uppercase letter to lowercase with an underspackages prefix
			if buf.Len() > 0 {
				buf.WriteRune('_')
			}
			buf.WriteRune(c - 'A' + 'a')
		} else {
			buf.WriteRune(c)
		}
	}
	return buf.String()
}

// ABOUT: Method to get table name from query
func GetTableNameFromQuery(query string) string {
	if strings.HasPrefix(query, "CREATE TABLE") {
		parts := strings.Fields(query)
		if len(parts) > 2 {
			tableName := parts[2]
			tableName = strings.Trim(tableName, "\"")
			return tableName
		}
	}
	return ""
}

// ABOUT: Method to check if a query is a SELECT query
func IsSelect(query string) bool {
	// You can use a simple heuristic to determine if a query is a SELECT query
	// For example, check if the query starts with "SELECT"
	query = strings.TrimSpace(query)
	return strings.HasPrefix(strings.ToUpper(query), "SELECT")
}
