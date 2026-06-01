package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	wrapperspb "google.golang.org/protobuf/types/known/wrapperspb"
	msgp "p9e.in/samavaya/packages/api/v1/message"
	"p9e.in/samavaya/packages/events/bus"
	"p9e.in/samavaya/packages/events/producer"
	"p9e.in/samavaya/packages/p9log"
)

// DomainEventPublisher handles publishing domain events to both local event bus and Kafka
type DomainEventPublisher struct {
	eventBus      *bus.EventBus
	kafkaProducer *producer.KafkaProducer
	logger        p9log.Helper
}

// NewDomainEventPublisher creates a new domain event publisher
func NewDomainEventPublisher(eventBus *bus.EventBus, kafkaProducer *producer.KafkaProducer, logger p9log.Logger) *DomainEventPublisher {
	return &DomainEventPublisher{
		eventBus:      eventBus,
		kafkaProducer: kafkaProducer,
		logger:        *p9log.NewHelper(p9log.With(logger, "component", "DomainEventPublisher")),
	}
}

// PublishEvent publishes a domain event to both local event bus and Kafka
func (p *DomainEventPublisher) PublishEvent(ctx context.Context, event *DomainEvent) error {
	// Publish to local event bus first for immediate processing
	if err := p.publishToEventBus(ctx, event); err != nil {
		p.logger.Errorf("Failed to publish event %s to local event bus: %v", event.ID, err)
		// Continue with Kafka publishing even if local bus fails
	}

	// Publish to Kafka for persistence and cross-service communication
	if p.kafkaProducer != nil {
		if err := p.publishToKafka(ctx, event); err != nil {
			p.logger.Errorf("Failed to publish event %s to Kafka: %v", event.ID, err)
			return fmt.Errorf("failed to publish event to Kafka: %w", err)
		}
	}

	p.logger.Infof("Successfully published domain event %s of type %s", event.ID, event.Type)
	return nil
}

// PublishWorkflowTransition publishes a workflow transition event
func (p *DomainEventPublisher) PublishWorkflowTransition(ctx context.Context, wte *WorkflowTransitionEvent) error {
	event := NewEventBuilder(EventTypeWorkflowTransition, wte.FormInstanceID, "form_instance").
		WithWorkflowTransition(wte).
		WithPriority(PriorityMedium).
		WithSource("formbuilder").
		Build()

	return p.PublishEvent(ctx, event)
}

// PublishSLABreach publishes an SLA breach event
func (p *DomainEventPublisher) PublishSLABreach(ctx context.Context, sla *SLAEvent) error {
	event := NewEventBuilder(EventTypeSLABreach, sla.FormInstanceID, "form_instance").
		WithSLA(sla).
		WithPriority(PriorityHigh).
		WithSource("monitoring").
		Build()

	return p.PublishEvent(ctx, event)
}

// PublishSLAWarning publishes an SLA warning event
func (p *DomainEventPublisher) PublishSLAWarning(ctx context.Context, sla *SLAEvent) error {
	event := NewEventBuilder(EventTypeSLAWarning, sla.FormInstanceID, "form_instance").
		WithSLA(sla).
		WithPriority(PriorityMedium).
		WithSource("monitoring").
		Build()

	return p.PublishEvent(ctx, event)
}

// PublishSLAEscalation publishes an SLA escalation event
func (p *DomainEventPublisher) PublishSLAEscalation(ctx context.Context, sla *SLAEvent) error {
	event := NewEventBuilder(EventTypeSLAEscalation, sla.FormInstanceID, "form_instance").
		WithSLA(sla).
		WithPriority(PriorityCritical).
		WithSource("monitoring").
		Build()

	return p.PublishEvent(ctx, event)
}

// PublishFormSubmission publishes a form submission event
func (p *DomainEventPublisher) PublishFormSubmission(ctx context.Context, fe *FormEvent) error {
	event := NewEventBuilder(EventTypeFormSubmission, fe.FormInstanceID, "form_instance").
		WithForm(fe).
		WithPriority(PriorityMedium).
		WithSource("formbuilder").
		Build()

	return p.PublishEvent(ctx, event)
}

// PublishNotificationSent publishes a notification sent event
func (p *DomainEventPublisher) PublishNotificationSent(ctx context.Context, ne *NotificationEvent) error {
	event := NewEventBuilder(EventTypeNotificationSent, ne.NotificationID, "notification").
		WithNotification(ne).
		WithPriority(PriorityLow).
		WithSource("notification").
		Build()

	return p.PublishEvent(ctx, event)
}

// PublishNotificationDelivered publishes a notification delivered event
func (p *DomainEventPublisher) PublishNotificationDelivered(ctx context.Context, ne *NotificationEvent) error {
	event := NewEventBuilder(EventTypeNotificationDelivered, ne.NotificationID, "notification").
		WithNotification(ne).
		WithPriority(PriorityLow).
		WithSource("notification").
		Build()

	return p.PublishEvent(ctx, event)
}

// PublishMetricRecorded publishes a metric recorded event
func (p *DomainEventPublisher) PublishMetricRecorded(ctx context.Context, me *MonitoringEvent) error {
	event := NewEventBuilder(EventTypeMetricRecorded, me.InstanceID, "service_instance").
		WithMonitoring(me).
		WithPriority(PriorityLow).
		WithSource("monitoring").
		Build()

	return p.PublishEvent(ctx, event)
}

// PublishAlertTriggered publishes an alert triggered event
func (p *DomainEventPublisher) PublishAlertTriggered(ctx context.Context, me *MonitoringEvent) error {
	priority := PriorityMedium
	if me.Severity == "critical" {
		priority = PriorityCritical
	} else if me.Severity == "high" {
		priority = PriorityHigh
	}

	event := NewEventBuilder(EventTypeAlertTriggered, me.InstanceID, "service_instance").
		WithMonitoring(me).
		WithPriority(priority).
		WithSource("monitoring").
		Build()

	return p.PublishEvent(ctx, event)
}

// PublishUserCreated publishes a user created event
func (p *DomainEventPublisher) PublishUserCreated(ctx context.Context, ie *IdentityEvent) error {
	event := NewEventBuilder(EventTypeUserCreated, ie.UserID, "user").
		WithIdentity(ie).
		WithPriority(PriorityMedium).
		WithSource("identity").
		Build()

	return p.PublishEvent(ctx, event)
}

// PublishTenantCreated publishes a tenant created event
func (p *DomainEventPublisher) PublishTenantCreated(ctx context.Context, te *TenantEvent) error {
	event := NewEventBuilder(EventTypeTenantCreated, te.TenantID, "tenant").
		WithTenant(te).
		WithPriority(PriorityMedium).
		WithSource("tenant").
		Build()

	return p.PublishEvent(ctx, event)
}

// PublishTenantUpdated publishes a tenant updated event
func (p *DomainEventPublisher) PublishTenantUpdated(ctx context.Context, te *TenantEvent) error {
	event := NewEventBuilder(EventTypeTenantUpdated, te.TenantID, "tenant").
		WithTenant(te).
		WithPriority(PriorityMedium).
		WithSource("tenant").
		Build()

	return p.PublishEvent(ctx, event)
}

// PublishTenantUserAdded publishes a tenant user added event
func (p *DomainEventPublisher) PublishTenantUserAdded(ctx context.Context, te *TenantEvent) error {
	event := NewEventBuilder(EventTypeTenantUserAdded, te.TenantID, "tenant").
		WithTenant(te).
		WithPriority(PriorityMedium).
		WithSource("tenant").
		Build()

	return p.PublishEvent(ctx, event)
}

// PublishSystemStartup publishes a system startup event
func (p *DomainEventPublisher) PublishSystemStartup(ctx context.Context, se *SystemEvent) error {
	event := NewEventBuilder(EventTypeSystemStartup, se.ServiceName, "service").
		WithSystem(se).
		WithPriority(PriorityMedium).
		WithSource(se.ServiceName).
		Build()

	return p.PublishEvent(ctx, event)
}

// publishToEventBus publishes the event to the local event bus
func (p *DomainEventPublisher) publishToEventBus(ctx context.Context, event *DomainEvent) error {
	publish := bus.Publish[*DomainEvent](p.eventBus)
	return publish(ctx, event)
}

// publishToKafka publishes the event to Kafka
func (p *DomainEventPublisher) publishToKafka(ctx context.Context, event *DomainEvent) error {
	// Convert domain event to Kafka message format
	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal domain event: %w", err)
	}

	anyValue, _ := anypb.New(wrapperspb.String(string(eventData)))
	kafkaMessage := &msgp.EventMessage{
		Key:       event.AggregateID,
		Value:     anyValue,
		Topic:     event.GetTopic(),
		Partition: 0,                                // Let Kafka decide partition based on key
		Offset:    0,                                // Will be set by Kafka
		EventTime: timestamppb.New(event.Timestamp), // Use timestamppb.Timestamp
	}

	return p.kafkaProducer.ProduceMessage(ctx, kafkaMessage)
}

// BatchPublishEvents publishes multiple events in a batch
func (p *DomainEventPublisher) BatchPublishEvents(ctx context.Context, events []*DomainEvent) error {
	var errors []error

	for _, event := range events {
		if err := p.PublishEvent(ctx, event); err != nil {
			errors = append(errors, fmt.Errorf("failed to publish event %s: %w", event.ID, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("batch publish failed with %d errors: %v", len(errors), errors)
	}

	p.logger.Infof("Successfully published batch of %d domain events", len(events))
	return nil
}

// PublishEventWithRetry publishes an event with retry logic
func (p *DomainEventPublisher) PublishEventWithRetry(ctx context.Context, event *DomainEvent, maxRetries int, retryDelay time.Duration) error {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := p.PublishEvent(ctx, event); err != nil {
			lastErr = err
			p.logger.Warnf("Failed to publish event %s (attempt %d/%d): %v", event.ID, attempt, maxRetries, err)

			if attempt < maxRetries {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(retryDelay):
					continue
				}
			}
		} else {
			return nil
		}
	}

	return fmt.Errorf("failed to publish event after %d attempts: %w", maxRetries, lastErr)
}

// EventSubscriber provides methods for subscribing to domain events
type EventSubscriber struct {
	eventBus *bus.EventBus
	logger   p9log.Helper
}

// NewEventSubscriber creates a new event subscriber
func NewEventSubscriber(eventBus *bus.EventBus, logger p9log.Logger) *EventSubscriber {
	return &EventSubscriber{
		eventBus: eventBus,
		logger:   *p9log.NewHelper(p9log.With(logger, "component", "EventSubscriber")),
	}
}

// SubscribeToWorkflowEvents subscribes to workflow-related events
func (s *EventSubscriber) SubscribeToWorkflowEvents(handler func(ctx context.Context, event *DomainEvent) error) (bus.IDisposable, error) {
	subscribe := bus.Subscribe[*DomainEvent](s.eventBus)
	return subscribe(func(ctx context.Context, event *DomainEvent) error {
		if s.isWorkflowEvent(event.Type) {
			s.logger.Infof("Processing workflow event %s of type %s", event.ID, event.Type)
			return handler(ctx, event)
		}
		return nil
	})
}

// SubscribeToSLAEvents subscribes to SLA-related events
func (s *EventSubscriber) SubscribeToSLAEvents(handler func(ctx context.Context, event *DomainEvent) error) (bus.IDisposable, error) {
	subscribe := bus.Subscribe[*DomainEvent](s.eventBus)
	return subscribe(func(ctx context.Context, event *DomainEvent) error {
		if s.isSLAEvent(event.Type) {
			s.logger.Infof("Processing SLA event %s of type %s", event.ID, event.Type)
			return handler(ctx, event)
		}
		return nil
	})
}

// SubscribeToNotificationEvents subscribes to notification-related events
func (s *EventSubscriber) SubscribeToNotificationEvents(handler func(ctx context.Context, event *DomainEvent) error) (bus.IDisposable, error) {
	subscribe := bus.Subscribe[*DomainEvent](s.eventBus)
	return subscribe(func(ctx context.Context, event *DomainEvent) error {
		if s.isNotificationEvent(event.Type) {
			s.logger.Infof("Processing notification event %s of type %s", event.ID, event.Type)
			return handler(ctx, event)
		}
		return nil
	})
}

// SubscribeToMonitoringEvents subscribes to monitoring-related events
func (s *EventSubscriber) SubscribeToMonitoringEvents(handler func(ctx context.Context, event *DomainEvent) error) (bus.IDisposable, error) {
	subscribe := bus.Subscribe[*DomainEvent](s.eventBus)
	return subscribe(func(ctx context.Context, event *DomainEvent) error {
		if s.isMonitoringEvent(event.Type) {
			s.logger.Infof("Processing monitoring event %s of type %s", event.ID, event.Type)
			return handler(ctx, event)
		}
		return nil
	})
}

// SubscribeToAllEvents subscribes to all domain events
func (s *EventSubscriber) SubscribeToAllEvents(handler func(ctx context.Context, event *DomainEvent) error) (bus.IDisposable, error) {
	subscribe := bus.Subscribe[*DomainEvent](s.eventBus)
	return subscribe(func(ctx context.Context, event *DomainEvent) error {
		s.logger.Infof("Processing domain event %s of type %s", event.ID, event.Type)
		return handler(ctx, event)
	})
}

// Helper methods to check event types
func (s *EventSubscriber) isWorkflowEvent(eventType EventType) bool {
	return eventType == EventTypeWorkflowTransition ||
		eventType == EventTypeWorkflowCreated ||
		eventType == EventTypeWorkflowUpdated ||
		eventType == EventTypeWorkflowDeleted
}

func (s *EventSubscriber) isSLAEvent(eventType EventType) bool {
	return eventType == EventTypeSLABreach ||
		eventType == EventTypeSLAWarning ||
		eventType == EventTypeSLAEscalation ||
		eventType == EventTypeSLACompliance ||
		eventType == EventTypeSLACreated ||
		eventType == EventTypeSLAUpdated
}

func (s *EventSubscriber) isNotificationEvent(eventType EventType) bool {
	return eventType == EventTypeNotificationSent ||
		eventType == EventTypeNotificationDelivered ||
		eventType == EventTypeNotificationFailed ||
		eventType == EventTypeNotificationRead
}

func (s *EventSubscriber) isMonitoringEvent(eventType EventType) bool {
	return eventType == EventTypeMetricRecorded ||
		eventType == EventTypeAlertTriggered ||
		eventType == EventTypeHealthCheckFailed ||
		eventType == EventTypePerformanceIssue
}
