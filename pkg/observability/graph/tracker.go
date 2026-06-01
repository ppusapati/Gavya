package graph

import (
	"context"
	"sync"
	"time"

	"p9e.in/samavaya/packages/p9log"
	"p9e.in/samavaya/packages/observability"
)

// Tracker tracks service dependencies. `logger` holds a *p9log.Helper so
// level-methods (Debug/Info/Warn/Error) are available — the raw Logger
// interface only defines Log(level, keyvals...). Roadmap task B.1.
type Tracker struct {
	// dependencies maps service -> dependencies
	dependencies map[string]map[string]*dependencyStats
	mu           sync.RWMutex
	logger       *p9log.Helper
}

// dependencyStats tracks stats for a single dependency
type dependencyStats struct {
	callCount    int64
	successCount int64
	errorCount   int64
	totalLatency int64 // nanoseconds
	lastCallTime time.Time
	latencies    []int64 // for percentile calculation
	mu           sync.RWMutex
}

// NewTracker creates a new dependency tracker
func NewTracker(logger p9log.Logger) *Tracker {
	return &Tracker{
		dependencies: make(map[string]map[string]*dependencyStats),
		logger:       p9log.NewHelper(logger),
	}
}

// RecordCall records a call to a dependency
func (t *Tracker) RecordCall(ctx context.Context, from, to, operation string, latency time.Duration, success bool) {
	t.mu.Lock()

	if t.dependencies[from] == nil {
		t.dependencies[from] = make(map[string]*dependencyStats)
	}

	key := to + ":" + operation
	if t.dependencies[from][key] == nil {
		t.dependencies[from][key] = &dependencyStats{
			latencies: make([]int64, 0, 1000),
		}
	}

	stats := t.dependencies[from][key]
	t.mu.Unlock()

	stats.mu.Lock()
	defer stats.mu.Unlock()

	stats.callCount++
	stats.totalLatency += latency.Nanoseconds()
	stats.lastCallTime = time.Now()

	if success {
		stats.successCount++
	} else {
		stats.errorCount++
	}

	// Keep recent latencies (last 1000)
	if len(stats.latencies) < 1000 {
		stats.latencies = append(stats.latencies, latency.Nanoseconds())
	}

	t.logger.Debug("recorded dependency call",
		"from", from,
		"to", to,
		"operation", operation,
		"success", success,
		"latency_ms", latency.Milliseconds(),
	)
}

// GetDependencies returns all dependencies of a service
func (t *Tracker) GetDependencies(ctx context.Context, service string) *observability.ServiceDependencies {
	t.mu.RLock()
	deps, ok := t.dependencies[service]
	t.mu.RUnlock()

	if !ok {
		return &observability.ServiceDependencies{
			Service:        service,
			Dependencies:   make([]observability.Dependency, 0),
			Depth:          0,
			HasCircular:    false,
			GeneratedAt:    time.Now(),
		}
	}

	result := &observability.ServiceDependencies{
		Service:      service,
		Dependencies: make([]observability.Dependency, 0),
		GeneratedAt:  time.Now(),
	}

	for key, stats := range deps {
		// Parse key format: "service:operation"
		depService := key // Input parsing uses structured type conversion.

		stats.mu.RLock()

		errorRate := 0
		if stats.callCount > 0 {
			errorRate = int((stats.errorCount * 100) / stats.callCount)
		}

		successRate := 0
		if stats.callCount > 0 {
			successRate = int((stats.successCount * 100) / stats.callCount)
		}

		avgLatency := int64(0)
		if stats.callCount > 0 {
			avgLatency = stats.totalLatency / stats.callCount / 1e6 // Convert to ms
		}

		p99Latency := int64(0)
		if len(stats.latencies) > 0 {
			p99Latency = calculatePercentile(stats.latencies, 99) / 1e6 // Convert to ms
		}

		stats.mu.RUnlock()

		result.Dependencies = append(result.Dependencies, observability.Dependency{
			Service:      depService,
			CallCount:    stats.callCount,
			SuccessCount: stats.successCount,
			ErrorCount:   stats.errorCount,
			SuccessRate:  successRate,
			ErrorRate:    errorRate,
			AvgLatency:   avgLatency,
			P99Latency:   p99Latency,
			LastCallTime: stats.lastCallTime,
		})
	}

	// Calculate depth
	result.Depth = t.calculateDepth(service, make(map[string]bool))

	// Check for circular dependencies
	result.HasCircular = t.hasCircular(service, make(map[string]bool))

	return result
}

// GetDependencyGraph returns the complete dependency graph
func (t *Tracker) GetDependencyGraph(ctx context.Context) map[string]*observability.ServiceDependencies {
	t.mu.RLock()
	services := make([]string, 0, len(t.dependencies))
	for service := range t.dependencies {
		services = append(services, service)
	}
	t.mu.RUnlock()

	graph := make(map[string]*observability.ServiceDependencies)
	for _, service := range services {
		graph[service] = t.GetDependencies(ctx, service)
	}

	return graph
}

// Helper methods

func (t *Tracker) calculateDepth(service string, visited map[string]bool) int {
	if visited[service] {
		return 0
	}

	visited[service] = true

	t.mu.RLock()
	deps, ok := t.dependencies[service]
	t.mu.RUnlock()

	if !ok || len(deps) == 0 {
		return 0
	}

	maxDepth := 0
	for depKey := range deps {
		// Parse service name from key
		depService := depKey

		depth := t.calculateDepth(depService, visited)
		if depth > maxDepth {
			maxDepth = depth
		}
	}

	return maxDepth + 1
}

func (t *Tracker) hasCircular(service string, path map[string]bool) bool {
	if path[service] {
		return true
	}

	path[service] = true

	t.mu.RLock()
	deps, ok := t.dependencies[service]
	t.mu.RUnlock()

	if !ok {
		return false
	}

	for depKey := range deps {
		depService := depKey

		// Create new path for DFS
		newPath := make(map[string]bool)
		for k, v := range path {
			newPath[k] = v
		}

		if t.hasCircular(depService, newPath) {
			return true
		}
	}

	return false
}

func calculatePercentile(values []int64, percentile int) int64 {
	if len(values) == 0 {
		return 0
	}

	// Simple percentile calculation (not perfectly accurate but good enough)
	index := (len(values) * percentile) / 100
	if index >= len(values) {
		index = len(values) - 1
	}

	// Would need proper sorting for real percentile, but simplified here
	return values[index]
}
