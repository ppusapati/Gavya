package registry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"p9e.in/samavaya/packages/p9log"
)

// Registry implements ServiceRegistry interface with cache support and event streaming.
//
// Logger field note (2026-04-19, roadmap B.1): stores a *p9log.Helper rather
// than the raw p9log.Logger interface. The interface only defines `Log(level,
// keyvals...) error`; the friendlier level-methods (Debug/Info/Warn/Error)
// live on *Helper. The constructor still accepts a p9log.Logger so callers
// can inject any logger implementation; we wrap it via p9log.NewHelper here.
type Registry struct {
	backend    RegistryBackend
	logger     *p9log.Helper
	opts       Options
	watchers   map[string][]chan RegistryEvent
	watchersMu sync.RWMutex

	// Stop signal for background workers
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// New creates a new service registry with the given backend
func New(backend RegistryBackend, logger p9log.Logger, opts ...Option) *Registry {
	options := DefaultOptions()
	for _, opt := range opts {
		opt(&options)
	}

	r := &Registry{
		backend:  backend,
		logger:   p9log.NewHelper(logger),
		opts:     options,
		watchers: make(map[string][]chan RegistryEvent),
		stopChan: make(chan struct{}),
	}

	// Start background workers if cleanup is enabled
	if options.EnableAutoCleanup {
		r.wg.Add(1)
		go r.cleanupWorker()
	}

	return r
}

// Register registers a new service instance
func (r *Registry) Register(ctx context.Context, instance *ServiceInstance) error {
	if instance.ID == "" {
		return fmt.Errorf("instance ID is required")
	}
	if instance.ServiceName == "" {
		return fmt.Errorf("service name is required")
	}
	if instance.Host == "" {
		return fmt.Errorf("host is required")
	}
	if instance.Port <= 0 || instance.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}

	// Set defaults
	if instance.Health == "" {
		instance.Health = HealthUp
	}
	if instance.CreatedAt.IsZero() {
		instance.CreatedAt = time.Now()
	}
	if instance.RegisteredAt.IsZero() {
		instance.RegisteredAt = time.Now()
	}
	if instance.LastHeartbeat.IsZero() {
		instance.LastHeartbeat = time.Now()
	}
	if instance.UpdatedAt.IsZero() {
		instance.UpdatedAt = time.Now()
	}

	// Register in backend
	err := r.backend.Register(ctx, instance)
	if err != nil {
		r.logger.Error("failed to register instance",
			"instance_id", instance.ID,
			"service_name", instance.ServiceName,
			"error", err,
		)
		return err
	}

	r.logger.Info("instance registered",
		"instance_id", instance.ID,
		"service_name", instance.ServiceName,
		"host", instance.Host,
		"port", instance.Port,
	)

	// Broadcast registration event
	r.broadcastEvent(instance.ServiceName, RegistryEvent{
		Type:      "registered",
		Instance:  instance,
		Timestamp: time.Now(),
	})

	return nil
}

// Deregister deregisters a service instance
func (r *Registry) Deregister(ctx context.Context, instanceID string) error {
	// Get the instance first to broadcast event
	instance, err := r.backend.GetInstance(ctx, instanceID)
	if err != nil {
		return err
	}

	err = r.backend.Deregister(ctx, instanceID)
	if err != nil {
		r.logger.Error("failed to deregister instance",
			"instance_id", instanceID,
			"error", err,
		)
		return err
	}

	r.logger.Info("instance deregistered", "instance_id", instanceID)

	// Broadcast deregistration event
	if instance != nil {
		r.broadcastEvent(instance.ServiceName, RegistryEvent{
			Type:      "deregistered",
			Instance:  instance,
			Timestamp: time.Now(),
		})
	}

	return nil
}

// GetInstance retrieves a specific instance by ID
func (r *Registry) GetInstance(ctx context.Context, instanceID string) (*ServiceInstance, error) {
	return r.backend.GetInstance(ctx, instanceID)
}

// GetInstances retrieves all healthy instances of a service
func (r *Registry) GetInstances(ctx context.Context, serviceName string) ([]*ServiceInstance, error) {
	return r.backend.GetInstances(ctx, serviceName)
}

// GetInstancesByHealth retrieves instances filtered by health status
func (r *Registry) GetInstancesByHealth(ctx context.Context, serviceName string, health HealthStatus) ([]*ServiceInstance, error) {
	return r.backend.GetInstancesByHealth(ctx, serviceName, health)
}

// GetAllInstances retrieves all registered instances
func (r *Registry) GetAllInstances(ctx context.Context) ([]*ServiceInstance, error) {
	return r.backend.GetAllInstances(ctx)
}

// UpdateHealth updates an instance's health status
func (r *Registry) UpdateHealth(ctx context.Context, instanceID string, health HealthStatus) error {
	// Get instance to broadcast event
	instance, err := r.backend.GetInstance(ctx, instanceID)
	if err != nil {
		return err
	}

	err = r.backend.UpdateHealth(ctx, instanceID, health)
	if err != nil {
		r.logger.Error("failed to update health",
			"instance_id", instanceID,
			"health", health,
			"error", err,
		)
		return err
	}

	if instance != nil {
		r.logger.Info("instance health updated",
			"instance_id", instanceID,
			"service_name", instance.ServiceName,
			"health", health,
		)

		// Broadcast health change event
		instance.Health = health
		instance.UpdatedAt = time.Now()
		r.broadcastEvent(instance.ServiceName, RegistryEvent{
			Type:      "health_changed",
			Instance:  instance,
			Timestamp: time.Now(),
		})
	}

	return nil
}

// UpdateMetadata updates an instance's metadata
func (r *Registry) UpdateMetadata(ctx context.Context, instanceID string, metadata map[string]string) error {
	instance, err := r.backend.GetInstance(ctx, instanceID)
	if err != nil {
		return err
	}

	err = r.backend.UpdateMetadata(ctx, instanceID, metadata)
	if err != nil {
		r.logger.Error("failed to update metadata",
			"instance_id", instanceID,
			"error", err,
		)
		return err
	}

	if instance != nil {
		r.logger.Info("instance metadata updated",
			"instance_id", instanceID,
			"service_name", instance.ServiceName,
		)

		instance.Metadata = metadata
		instance.UpdatedAt = time.Now()
		r.broadcastEvent(instance.ServiceName, RegistryEvent{
			Type:      "metadata_updated",
			Instance:  instance,
			Timestamp: time.Now(),
		})
	}

	return nil
}

// Heartbeat records a heartbeat from an instance
func (r *Registry) Heartbeat(ctx context.Context, instanceID string) error {
	return r.backend.Heartbeat(ctx, instanceID)
}

// Watch watches for changes to a service
func (r *Registry) Watch(ctx context.Context, serviceName string) (<-chan RegistryEvent, error) {
	if serviceName == "" {
		return nil, fmt.Errorf("service name is required")
	}

	ch := make(chan RegistryEvent, 10) // Buffered to avoid blocking

	r.watchersMu.Lock()
	defer r.watchersMu.Unlock()

	// Check watcher limit
	if len(r.watchers[serviceName]) >= r.opts.MaxWatchers {
		return nil, fmt.Errorf("max watchers (%d) reached for service %s", r.opts.MaxWatchers, serviceName)
	}

	r.watchers[serviceName] = append(r.watchers[serviceName], ch)

	// Remove watcher when context is cancelled
	go func() {
		<-ctx.Done()
		r.removeWatcher(serviceName, ch)
		close(ch)
	}()

	return ch, nil
}

// Close closes the registry and releases resources
func (r *Registry) Close() error {
	close(r.stopChan)
	r.wg.Wait()

	// Close all watchers
	r.watchersMu.Lock()
	for _, watchers := range r.watchers {
		for _, ch := range watchers {
			close(ch)
		}
	}
	r.watchers = make(map[string][]chan RegistryEvent)
	r.watchersMu.Unlock()

	return r.backend.Close()
}

// broadcastEvent sends an event to all watchers for a service
func (r *Registry) broadcastEvent(serviceName string, event RegistryEvent) {
	r.watchersMu.RLock()
	defer r.watchersMu.RUnlock()

	watchers := r.watchers[serviceName]
	for _, ch := range watchers {
		select {
		case ch <- event:
		default:
			// Non-blocking send, skip if watcher is slow
		}
	}
}

// removeWatcher removes a watcher from the registry
func (r *Registry) removeWatcher(serviceName string, ch chan RegistryEvent) {
	r.watchersMu.Lock()
	defer r.watchersMu.Unlock()

	watchers := r.watchers[serviceName]
	for i, watcher := range watchers {
		if watcher == ch {
			r.watchers[serviceName] = append(watchers[:i], watchers[i+1:]...)
			break
		}
	}
}

// cleanupWorker periodically removes stale instances
func (r *Registry) cleanupWorker() {
	defer r.wg.Done()

	ticker := time.NewTicker(r.opts.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopChan:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			count, err := r.backend.CleanupStale(ctx, r.opts.HeartbeatTTL)
			cancel()

			if err != nil {
				r.logger.Error("cleanup failed", "error", err)
			} else if count > 0 {
				r.logger.Info("cleanup completed", "removed_count", count)
			}
		}
	}
}

// Option is a functional option for configuring the registry
type Option func(*Options)

// WithHeartbeatTTL sets the heartbeat TTL
func WithHeartbeatTTL(ttl time.Duration) Option {
	return func(o *Options) {
		o.HeartbeatTTL = ttl
	}
}

// WithCleanupInterval sets the cleanup interval
func WithCleanupInterval(interval time.Duration) Option {
	return func(o *Options) {
		o.CleanupInterval = interval
	}
}

// WithCacheTTL sets the cache TTL
func WithCacheTTL(ttl time.Duration) Option {
	return func(o *Options) {
		o.CacheTTL = ttl
	}
}

// WithMaxWatchers sets the maximum number of watchers
func WithMaxWatchers(max int) Option {
	return func(o *Options) {
		o.MaxWatchers = max
	}
}

// WithAutoCleanup enables or disables automatic cleanup
func WithAutoCleanup(enabled bool) Option {
	return func(o *Options) {
		o.EnableAutoCleanup = enabled
	}
}
