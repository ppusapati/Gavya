package alerts

import (
	"context"
	"fmt"
	"sync"
	"time"

	"p9e.in/samavaya/packages/p9log"
	"p9e.in/samavaya/packages/observability"
	"p9e.in/samavaya/packages/observability/metrics"
)

// Engine manages alert rules and firing
// Engine evaluates rules against metrics and dispatches alerts.
// `logger` stores *p9log.Helper for Info/Warn/Error (B.1 sweep).
type Engine struct {
	rules       map[string]*observability.AlertRule
	alerts      map[string]*observability.Alert
	recipients  []observability.AlertRecipient
	collector   *metrics.Collector
	logger      *p9log.Helper
	mu          sync.RWMutex
	stopChan    chan struct{}
	onAlert     func(ctx context.Context, alert *observability.Alert)
	cooldowns   map[string]time.Time // Track cooldown per rule
}

// NewEngine creates a new alert engine
func NewEngine(collector *metrics.Collector, logger p9log.Logger) *Engine {
	return &Engine{
		rules:      make(map[string]*observability.AlertRule),
		alerts:     make(map[string]*observability.Alert),
		recipients: make([]observability.AlertRecipient, 0),
		collector:  collector,
		logger:     p9log.NewHelper(logger),
		stopChan:   make(chan struct{}),
		cooldowns:  make(map[string]time.Time),
	}
}

// AddRule adds an alert rule
func (e *Engine) AddRule(rule observability.AlertRule) error {
	if rule.Name == "" {
		return fmt.Errorf("rule name is required")
	}

	if rule.Metric == "" {
		return fmt.Errorf("metric is required")
	}

	if rule.Op == "" {
		rule.Op = ">"
	}

	if rule.Duration == 0 {
		rule.Duration = 5 * time.Minute
	}

	if rule.Cooldown == 0 {
		rule.Cooldown = 1 * time.Minute
	}

	if rule.Labels == nil {
		rule.Labels = make(map[string]string)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.rules[rule.Name] = &rule

	e.logger.Info("added alert rule",
		"rule_name", rule.Name,
		"metric", rule.Metric,
		"threshold", rule.Threshold,
	)

	return nil
}

// RemoveRule removes an alert rule
func (e *Engine) RemoveRule(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	delete(e.rules, name)
	e.logger.Info("removed alert rule", "rule_name", name)
}

// AddRecipient adds an alert recipient
func (e *Engine) AddRecipient(recipient observability.AlertRecipient) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.recipients = append(e.recipients, recipient)
	e.logger.Info("added alert recipient",
		"type", recipient.Type,
		"address", recipient.Address,
	)
}

// Start begins the background alert checking
func (e *Engine) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.checkRules(ctx)
		}
	}
}

// Stop stops the alert engine
func (e *Engine) Stop() {
	close(e.stopChan)
}

// SetOnAlert sets the callback for fired alerts
func (e *Engine) SetOnAlert(callback func(ctx context.Context, alert *observability.Alert)) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.onAlert = callback
}

// GetAlert returns an alert by ID
func (e *Engine) GetAlert(alertID string) *observability.Alert {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.alerts[alertID]
}

// GetActiveAlerts returns all active (firing) alerts
func (e *Engine) GetActiveAlerts() []*observability.Alert {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var active []*observability.Alert
	for _, alert := range e.alerts {
		if alert.Status == observability.StatusFiring {
			active = append(active, alert)
		}
	}

	return active
}

// Helper methods

func (e *Engine) checkRules(ctx context.Context) {
	e.mu.RLock()
	rules := make(map[string]*observability.AlertRule)
	for name, rule := range e.rules {
		if rule.Enabled {
			rules[name] = rule
		}
	}
	cooldowns := make(map[string]time.Time)
	for rule, cooldown := range e.cooldowns {
		cooldowns[rule] = cooldown
	}
	e.mu.RUnlock()

	// Get current metrics
	snapshot, err := e.collector.GetSnapshot(ctx)
	if err != nil {
		e.logger.Error("failed to get metrics snapshot", "error", err)
		return
	}

	// Check each rule
	for ruleName, rule := range rules {
		// Check cooldown
		if cooldown, ok := cooldowns[ruleName]; ok && time.Now().Before(cooldown) {
			continue
		}

		// Find metric
		var metricValue *float64
		for _, metric := range snapshot.Metrics {
			if metric.Name == rule.Metric {
				metricValue = &metric.Value
				break
			}
		}

		if metricValue == nil {
			continue
		}

		// Evaluate condition
		triggered := e.evaluateCondition(*metricValue, rule.Op, rule.Threshold)

		if triggered {
			e.fireAlert(ctx, ruleName, rule, *metricValue)

			// Set cooldown
			e.mu.Lock()
			e.cooldowns[ruleName] = time.Now().Add(rule.Cooldown)
			e.mu.Unlock()
		}
	}
}

func (e *Engine) evaluateCondition(value float64, op string, threshold float64) bool {
	switch op {
	case ">":
		return value > threshold
	case "<":
		return value < threshold
	case ">=":
		return value >= threshold
	case "<=":
		return value <= threshold
	case "==":
		return value == threshold
	case "!=":
		return value != threshold
	default:
		return false
	}
}

func (e *Engine) fireAlert(ctx context.Context, ruleName string, rule *observability.AlertRule, value float64) {
	alertID := fmt.Sprintf("%s-%d", ruleName, time.Now().UnixNano())

	alert := &observability.Alert{
		Name:      ruleName,
		Status:    observability.StatusFiring,
		Message:   fmt.Sprintf("Alert %s fired: %s %s %v (current: %v)", ruleName, rule.Metric, rule.Op, rule.Threshold, value),
		Value:     value,
		FiredAt:   time.Now(),
		Labels:    rule.Labels,
		Rule:      rule,
		Severity:  e.determineSeverity(rule),
	}

	e.mu.Lock()
	e.alerts[alertID] = alert
	e.mu.Unlock()

	e.logger.Warn("alert fired",
		"alert_id", alertID,
		"rule_name", ruleName,
		"metric", rule.Metric,
		"value", value,
		"threshold", rule.Threshold,
	)

	// Call callback
	if e.onAlert != nil {
		e.onAlert(ctx, alert)
	}

	// Send to recipients
	e.notifyRecipients(ctx, alert)
}

func (e *Engine) notifyRecipients(ctx context.Context, alert *observability.Alert) {
	e.mu.RLock()
	recipients := make([]observability.AlertRecipient, len(e.recipients))
	copy(recipients, e.recipients)
	e.mu.RUnlock()

	for _, recipient := range recipients {
		// Check minimum severity
		if shouldNotify(alert.Severity, recipient.MinSeverity) {
			e.logger.Info("sending alert notification",
				"recipient", recipient.Type,
				"alert", alert.Name,
			)

			// Send to recipient (implementation would depend on type)
			// Slack: post message
			// Email: send email
			// Webhook: POST request
			// PagerDuty: create incident
		}
	}
}

func (e *Engine) determineSeverity(rule *observability.AlertRule) observability.AlertSeverity {
	if rule.Labels["severity"] == "critical" {
		return observability.SeverityCritical
	} else if rule.Labels["severity"] == "warning" {
		return observability.SeverityWarning
	}
	return observability.SeverityInfo
}

func shouldNotify(alertSeverity, minSeverity observability.AlertSeverity) bool {
	severityOrder := map[observability.AlertSeverity]int{
		observability.SeverityInfo:     1,
		observability.SeverityWarning:  2,
		observability.SeverityCritical: 3,
	}

	return severityOrder[alertSeverity] >= severityOrder[minSeverity]
}
