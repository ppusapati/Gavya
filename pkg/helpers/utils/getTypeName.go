package helpers_utils

import "fmt"

// Utility: Get type name
func GetTypeName[T any]() string {
	var t T
	return fmt.Sprintf("%T", t)
}
