package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"p9e.in/samavaya/packages/p9log"
	"p9e.in/samavaya/packages/loadbalancer"
	"p9e.in/samavaya/packages/mesh"
)

// PolicyServer implements HTTP API for mesh policy management.
// `logger` is *p9log.Helper (B.1 sweep).
type PolicyServer struct {
	mesh   *mesh.ServiceMesh
	logger *p9log.Helper
	mux    *http.ServeMux
}

// New creates a new policy server
func New(m *mesh.ServiceMesh, logger p9log.Logger) *PolicyServer {
	s := &PolicyServer{
		mesh:   m,
		logger: p9log.NewHelper(logger),
		mux:    http.NewServeMux(),
	}

	// Register handlers
	s.mux.HandleFunc("GET /v1/policies/{serviceName}", s.handleGetPolicy)
	s.mux.HandleFunc("PUT /v1/policies/{serviceName}", s.handleSetPolicy)
	s.mux.HandleFunc("GET /v1/policies", s.handleListPolicies)

	return s
}

// ServeHTTP implements http.Handler
func (s *PolicyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Request/Response types

type SetPolicyRequest struct {
	ServiceName            string                           `json:"service_name"`
	VersionConstraint      string                           `json:"version_constraint,omitempty"`
	Region                 string                           `json:"region,omitempty"`
	LoadBalancingAlgorithm loadbalancer.Algorithm           `json:"load_balancing_algorithm"`
	CircuitBreakerConfig   mesh.CircuitBreakerConfig        `json:"circuit_breaker_config"`
	RetryPolicy            mesh.RetryPolicy                 `json:"retry_policy"`
	TimeoutPolicy          mesh.TimeoutPolicy               `json:"timeout_policy"`
	CanaryConfig           *mesh.CanaryConfig               `json:"canary_config,omitempty"`
	Metadata               map[string]string                `json:"metadata,omitempty"`
}

type PolicyResponse struct {
	ServiceName            string                           `json:"service_name"`
	VersionConstraint      string                           `json:"version_constraint,omitempty"`
	Region                 string                           `json:"region,omitempty"`
	LoadBalancingAlgorithm loadbalancer.Algorithm           `json:"load_balancing_algorithm"`
	CircuitBreakerConfig   mesh.CircuitBreakerConfig        `json:"circuit_breaker_config"`
	RetryPolicy            mesh.RetryPolicy                 `json:"retry_policy"`
	TimeoutPolicy          mesh.TimeoutPolicy               `json:"timeout_policy"`
	CanaryConfig           *mesh.CanaryConfig               `json:"canary_config,omitempty"`
	Metadata               map[string]string                `json:"metadata,omitempty"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

// Handlers

func (s *PolicyServer) handleGetPolicy(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	serviceName := r.PathValue("serviceName")
	if serviceName == "" {
		s.writeError(w, http.StatusBadRequest, "missing_field", "serviceName is required")
		return
	}

	policy, err := s.mesh.GetPolicy(ctx, serviceName)
	if err != nil {
		s.logger.Error("failed to get policy",
			"service_name", serviceName,
			"error", err,
		)
		s.writeError(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}

	resp := PolicyResponse{
		ServiceName:            policy.ServiceName,
		VersionConstraint:      policy.VersionConstraint,
		Region:                 policy.Region,
		LoadBalancingAlgorithm: policy.LoadBalancingAlgorithm,
		CircuitBreakerConfig:   policy.CircuitBreakerConfig,
		RetryPolicy:            policy.RetryPolicy,
		TimeoutPolicy:          policy.TimeoutPolicy,
		CanaryConfig:           policy.CanaryConfig,
		Metadata:               policy.Metadata,
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *PolicyServer) handleSetPolicy(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	serviceName := r.PathValue("serviceName")
	if serviceName == "" {
		s.writeError(w, http.StatusBadRequest, "missing_field", "serviceName is required")
		return
	}

	var req SetPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "Failed to decode request body")
		return
	}

	// Validate request
	if req.LoadBalancingAlgorithm == "" {
		req.LoadBalancingAlgorithm = loadbalancer.AlgorithmRoundRobin
	}

	// If circuit breaker config is empty, use defaults
	if req.CircuitBreakerConfig.FailureThreshold == 0 {
		req.CircuitBreakerConfig = mesh.DefaultCircuitBreakerConfig()
	}

	// If retry policy is empty, use defaults
	if req.RetryPolicy.MaxAttempts == 0 {
		req.RetryPolicy = mesh.DefaultRetryPolicy()
	}

	// If timeout policy is empty, use defaults
	if req.TimeoutPolicy.ConnectTimeout == 0 {
		req.TimeoutPolicy = mesh.DefaultTimeoutPolicy()
	}

	// Create policy
	policy := &mesh.RoutingPolicy{
		ServiceName:            serviceName,
		VersionConstraint:      req.VersionConstraint,
		Region:                 req.Region,
		LoadBalancingAlgorithm: req.LoadBalancingAlgorithm,
		CircuitBreakerConfig:   req.CircuitBreakerConfig,
		RetryPolicy:            req.RetryPolicy,
		TimeoutPolicy:          req.TimeoutPolicy,
		CanaryConfig:           req.CanaryConfig,
		Metadata:               req.Metadata,
	}

	// Set policy
	if err := s.mesh.SetPolicy(ctx, policy); err != nil {
		s.logger.Error("failed to set policy",
			"service_name", serviceName,
			"error", err,
		)
		s.writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}

	resp := PolicyResponse{
		ServiceName:            policy.ServiceName,
		VersionConstraint:      policy.VersionConstraint,
		Region:                 policy.Region,
		LoadBalancingAlgorithm: policy.LoadBalancingAlgorithm,
		CircuitBreakerConfig:   policy.CircuitBreakerConfig,
		RetryPolicy:            policy.RetryPolicy,
		TimeoutPolicy:          policy.TimeoutPolicy,
		CanaryConfig:           policy.CanaryConfig,
		Metadata:               policy.Metadata,
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *PolicyServer) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	// Placeholder for listing all policies
	// Would need to add method to ServiceMesh to return all policies
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"policies": []interface{}{},
	})
}

// Helper methods

func (s *PolicyServer) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *PolicyServer) writeError(w http.ResponseWriter, status int, code string, message string) {
	resp := ErrorResponse{
		Error:   code,
		Message: message,
		Code:    code,
	}
	s.writeJSON(w, status, resp)
}
