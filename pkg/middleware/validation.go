package middleware

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ValidationMiddleware validates incoming requests
type ValidationMiddleware struct {
	validators map[string]RequestValidator
}

// RequestValidator validates a request
type RequestValidator interface {
	Validate(req interface{}) error
}

// NewValidationMiddleware creates a new validation middleware
func NewValidationMiddleware() *ValidationMiddleware {
	return &ValidationMiddleware{
		validators: make(map[string]RequestValidator),
	}
}

// RegisterValidator registers a validator for a method
func (v *ValidationMiddleware) RegisterValidator(method string, validator RequestValidator) {
	v.validators[method] = validator
}

// UnaryInterceptor returns a unary RPC interceptor for validation
func (v *ValidationMiddleware) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (interface{}, error) {

		// Run validation if registered
		if validator, exists := v.validators[info.FullMethod]; exists {
			if err := validator.Validate(req); err != nil {
				return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("validation error: %v", err))
			}
		}

		return handler(ctx, req)
	}
}

// CustomValidator implements validation logic
type CustomValidator struct {
	rules map[string]ValidationRule
}

// ValidationRule defines a validation rule
type ValidationRule struct {
	Name    string
	Checker func(interface{}) bool
	Message string
}

// Validate checks all rules
func (c *CustomValidator) Validate(req interface{}) error {
	for _, rule := range c.rules {
		if !rule.Checker(req) {
			return fmt.Errorf(rule.Message)
		}
	}
	return nil
}

// NewParcelValidator creates a validator for parcel creation
func NewParcelValidator() RequestValidator {
	return &CustomValidator{
		rules: map[string]ValidationRule{
			"survey_number": {
				Name: "survey_number",
				Checker: func(req interface{}) bool {
					// TODO: Implement survey number validation
					return true
				},
				Message: "Invalid survey number format",
			},
			"area": {
				Name: "area",
				Checker: func(req interface{}) bool {
					// TODO: Check area > 0
					return true
				},
				Message: "Area must be greater than zero",
			},
			"coordinates": {
				Name: "coordinates",
				Checker: func(req interface{}) bool {
					// TODO: Validate lat/lng ranges
					return true
				},
				Message: "Invalid coordinates",
			},
		},
	}
}
