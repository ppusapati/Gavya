package helpers_utils

import (
	"time"

	"p9e.in/samavaya/packages/models"
)

// UpdateRequest represents the data needed for an update operation
type UpdateRequest[T models.Entity] struct {
	Entity    T
	FieldMask []string
	UpdatedBy string    // ppusapati
	UpdatedAt time.Time // 2025-03-21 11:33:23
}
