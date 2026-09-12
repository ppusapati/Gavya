//go:build e2e

// notification-service and reporting-service, which are each a queue with a
// lifecycle.
//
// Both are thin, and both have the shape where a defect hides best: a list with
// optional filters, and a state that moves once. ListNotifications already
// carries a defect of exactly that kind in its history — it compared channel and
// status unconditionally, so the obvious call with no filters returned an empty
// list, which is indistinguishable from a tenant with no notifications.
//
// So the assertions here are about filters that filter and states that move, not
// about rows coming back.
package e2e

import (
	"context"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const (
	notifySvc = "notification.v1.NotificationService"
	reportSvc = "reporting.v1.ReportingService"
)

type markReadReq struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	UpdatedBy string `json:"updated_by"`
}

type markAllReadReq struct {
	RecipientID string `json:"recipient_id"`
	TenantID    string `json:"tenant_id"`
	UpdatedBy   string `json:"updated_by"`
}

type unreadCountReq struct {
	TenantID    string `json:"tenant_id"`
	RecipientID string `json:"recipient_id"`
}

type unreadCountResp struct {
	Count int `json:"count"`
}

type createTemplateReq struct {
	TenantID     string `json:"tenant_id"`
	EventType    string `json:"event_type"`
	Channel      string `json:"channel"`
	Title        string `json:"title"`
	BodyTemplate string `json:"body_template"`
	CreatedBy    string `json:"created_by"`
}

type templateResp struct {
	Template *struct {
		ID        string `json:"id"`
		EventType string `json:"event_type"`
		Channel   string `json:"channel"`
	} `json:"template"`
}

type listTemplatesResp struct {
	Templates []*struct {
		ID        string `json:"id"`
		EventType string `json:"event_type"`
	} `json:"templates"`
}

func notify(t *testing.T, p *platform, recipient, channel string) *notificationResp {
	t.Helper()
	out, err := svcclient.Call[sendNotificationReq, notificationResp](
		context.Background(), p.notification(), notifySvc+"/SendNotification",
		sendNotificationReq{TenantID: p.tenant, RecipientID: recipient,
			RecipientType: "user", Channel: channel, Title: "collection recorded",
			Body: "12.400 litres", Priority: "normal", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("send a %s notification: %v", channel, err)
	}
	return out
}

// A notification is read once, and the unread count follows.
//
// The count is what a device badges. One that does not follow the reads is a
// number somebody clears by opening the app and finds unchanged.
func TestANotificationIsReadAndTheCountFollows(t *testing.T) {
	p := startPlatform(t)
	recipient := newID("usr")

	first := notify(t, p, recipient, "sms")
	notify(t, p, recipient, "email")

	before, err := svcclient.Call[unreadCountReq, unreadCountResp](
		context.Background(), p.notification(), notifySvc+"/GetUnreadCount",
		unreadCountReq{TenantID: p.tenant, RecipientID: recipient}, p.opts())
	if err != nil {
		t.Fatalf("unread count: %v", err)
	}
	if before.Count != 2 {
		t.Fatalf("the recipient has %d unread, want 2", before.Count)
	}

	got, err := svcclient.Call[markReadReq, notificationResp](
		context.Background(), p.notification(), notifySvc+"/MarkAsRead",
		markReadReq{ID: first.Notification.ID, TenantID: p.tenant, UpdatedBy: "e2e"},
		p.opts())
	if err != nil {
		t.Fatalf("mark as read: %v", err)
	}
	if got.Notification.Status == first.Notification.Status {
		t.Errorf("the notification is still %s after being read", got.Notification.Status)
	}

	after, err := svcclient.Call[unreadCountReq, unreadCountResp](
		context.Background(), p.notification(), notifySvc+"/GetUnreadCount",
		unreadCountReq{TenantID: p.tenant, RecipientID: recipient}, p.opts())
	if err != nil {
		t.Fatalf("unread count: %v", err)
	}
	if after.Count != 1 {
		t.Errorf("after reading one of two the count is %d, want 1 — a badge that does "+
			"not follow the reads is one somebody clears and finds unchanged", after.Count)
	}

	// And the rest go at once.
	if _, err := svcclient.Call[markAllReadReq, struct {
		Status string `json:"status"`
	}](
		context.Background(), p.notification(), notifySvc+"/MarkAllRead",
		markAllReadReq{RecipientID: recipient, TenantID: p.tenant, UpdatedBy: "e2e"},
		p.opts()); err != nil {
		t.Fatalf("mark all read: %v", err)
	}
	none, err := svcclient.Call[unreadCountReq, unreadCountResp](
		context.Background(), p.notification(), notifySvc+"/GetUnreadCount",
		unreadCountReq{TenantID: p.tenant, RecipientID: recipient}, p.opts())
	if err != nil {
		t.Fatalf("unread count: %v", err)
	}
	if none.Count != 0 {
		t.Errorf("%d unread remain after marking all read", none.Count)
	}
}

// The unread count is one recipient's.
//
// A count that ignores the recipient is the same number for everybody, and it is
// the right order of magnitude, which is what makes it survive.
func TestTheUnreadCountIsOneRecipients(t *testing.T) {
	p := startPlatform(t)
	mine, theirs := newID("usr"), newID("usr")
	notify(t, p, mine, "sms")
	for i := 0; i < 3; i++ {
		notify(t, p, theirs, "sms")
	}

	got, err := svcclient.Call[unreadCountReq, unreadCountResp](
		context.Background(), p.notification(), notifySvc+"/GetUnreadCount",
		unreadCountReq{TenantID: p.tenant, RecipientID: mine}, p.opts())
	if err != nil {
		t.Fatalf("unread count: %v", err)
	}
	if got.Count != 1 {
		t.Errorf("one recipient has %d unread, want 1; another has three", got.Count)
	}
}

// A filter that is set filters, and one that is not does not.
//
// ListNotifications compared channel and status unconditionally, so a caller
// passing neither — which the request type invites, since neither is required —
// got an empty list. An empty list is indistinguishable from a tenant with no
// notifications, so the obvious call returned the obviously wrong answer and
// looked right doing it.
func TestANotificationFilterFiltersOnlyWhenItIsSet(t *testing.T) {
	p := startPlatform(t)
	recipient := newID("usr")
	notify(t, p, recipient, "sms")
	notify(t, p, recipient, "sms")
	notify(t, p, recipient, "email")

	all, err := svcclient.Call[listNotificationsReq, listNotificationsResp](
		context.Background(), p.notification(), notifySvc+"/ListNotifications",
		listNotificationsReq{TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("list with no filters: %v", err)
	}
	if len(all.Notifications) < 3 {
		t.Fatalf("an unfiltered list holds %d of at least 3 notifications; an unset "+
			"filter means \"not filtering by that\", not \"match the empty string\"",
			len(all.Notifications))
	}

	sms, err := svcclient.Call[listNotificationsReq, listNotificationsResp](
		context.Background(), p.notification(), notifySvc+"/ListNotifications",
		listNotificationsReq{TenantID: p.tenant, Channel: "sms"}, p.opts())
	if err != nil {
		t.Fatalf("list by channel: %v", err)
	}
	if len(sms.Notifications) >= len(all.Notifications) {
		t.Errorf("filtering by channel returned %d of %d — the filter did not filter",
			len(sms.Notifications), len(all.Notifications))
	}
	for _, n := range sms.Notifications {
		if n.Channel != "sms" {
			t.Errorf("an %s notification came back in the sms list", n.Channel)
		}
	}
}

// A template is created and listed, and belongs to its tenant.
func TestANotificationTemplateIsCreatedAndListed(t *testing.T) {
	p := startPlatform(t)
	event := newID("evt")

	made, err := svcclient.Call[createTemplateReq, templateResp](
		context.Background(), p.notification(), notifySvc+"/CreateTemplate",
		createTemplateReq{TenantID: p.tenant, EventType: event, Channel: "sms",
			Title:        "collection recorded",
			BodyTemplate: "{{.Litres}} litres from {{.Producer}}",
			CreatedBy:    "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	if made.Template.EventType != event {
		t.Errorf("the template is for %s, want %s", made.Template.EventType, event)
	}

	list, err := svcclient.Call[tenantReq, listTemplatesResp](
		context.Background(), p.notification(), notifySvc+"/ListTemplates",
		tenantReq{TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("list templates: %v", err)
	}
	var found bool
	for _, tpl := range list.Templates {
		if tpl.ID == made.Template.ID {
			found = true
		}
	}
	if !found {
		t.Error("a template that was just created is not in the list")
	}

	other := newID("tnt")
	theirs, err := svcclient.Call[tenantReq, listTemplatesResp](
		context.Background(), p.notification(), notifySvc+"/ListTemplates",
		tenantReq{TenantID: other}, actingAs(other, "e2e"))
	if err == nil && len(theirs.Templates) > 0 {
		t.Errorf("a second tenant sees %d templates it did not write", len(theirs.Templates))
	}
}

// ---------------------------------------------------------------------------
// reporting
// ---------------------------------------------------------------------------

type downloadURLResp struct {
	URL string `json:"url"`
}

type createScheduleReq struct {
	TenantID   string `json:"tenant_id"`
	ReportType string `json:"report_type"`
	Schedule   string `json:"schedule"`
	Parameters string `json:"parameters"`
	CreatedBy  string `json:"created_by"`
}

type scheduleResp struct {
	Schedule *struct {
		ID         string `json:"id"`
		ReportType string `json:"report_type"`
		IsActive   bool   `json:"is_active"`
	} `json:"schedule"`
}

type listSchedulesResp struct {
	Schedules []*struct {
		ID       string `json:"id"`
		IsActive bool   `json:"is_active"`
	} `json:"schedules"`
}

type scheduleActionReq struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	IsActive  bool   `json:"is_active"`
	UpdatedBy string `json:"updated_by"`
}

// A report is requested and read back, and a download is only offered for one
// that finished.
//
// A URL handed out for a report still running points at a file that is not
// there, and whoever follows it sees an error rather than the thing they asked
// for.
func TestAReportIsRequestedAndOnlyOfferedWhenItIsDone(t *testing.T) {
	p := startPlatform(t)

	made, err := svcclient.Call[requestReportReq, reportResp](
		context.Background(), p.reporting(), reportSvc+"/RequestReport",
		requestReportReq{TenantID: p.tenant, Name: "fortnight collections",
			ReportType: "collections", Parameters: `{"from":"2026-02-01"}`,
			FileFormat: "csv", RequestedBy: "e2e", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("request report: %v", err)
	}

	got, err := svcclient.Call[idTenantReq, reportResp](
		context.Background(), p.reporting(), reportSvc+"/GetReport",
		idTenantReq{ID: made.Report.ID, TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("get report: %v", err)
	}
	if got.Report.Name != "fortnight collections" {
		t.Errorf("the report reads back as %q", got.Report.Name)
	}

	// It has only just been asked for, so there is nothing to download.
	if _, err := svcclient.Call[idTenantReq, downloadURLResp](
		context.Background(), p.reporting(), reportSvc+"/GetReportDownloadURL",
		idTenantReq{ID: made.Report.ID, TenantID: p.tenant}, p.opts()); err == nil {
		t.Error("a download was offered for a report that has not finished; the URL " +
			"points at a file that is not there")
	}

	// And a second tenant cannot read it.
	other := newID("tnt")
	if _, err := svcclient.Call[idTenantReq, reportResp](
		context.Background(), p.reporting(), reportSvc+"/GetReport",
		idTenantReq{ID: made.Report.ID, TenantID: other},
		actingAs(other, "e2e")); err == nil {
		t.Error("a second tenant read a report it did not request")
	}
}

// A schedule is created, switched off, and deleted.
func TestAReportScheduleIsCreatedSwitchedOffAndDeleted(t *testing.T) {
	p := startPlatform(t)

	made, err := svcclient.Call[createScheduleReq, scheduleResp](
		context.Background(), p.reporting(), reportSvc+"/CreateSchedule",
		createScheduleReq{TenantID: p.tenant, ReportType: "collections",
			Schedule: "0 6 * * *", Parameters: `{}`, CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	if !made.Schedule.IsActive {
		t.Error("a new schedule is inactive; one that never runs is one nobody notices " +
			"is not running")
	}

	off, err := svcclient.Call[scheduleActionReq, scheduleResp](
		context.Background(), p.reporting(), reportSvc+"/UpdateSchedule",
		scheduleActionReq{ID: made.Schedule.ID, TenantID: p.tenant,
			IsActive: false, UpdatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("update schedule: %v", err)
	}
	if off.Schedule.IsActive {
		t.Error("the schedule is still active after being switched off")
	}

	if _, err := svcclient.Call[scheduleActionReq, struct{}](
		context.Background(), p.reporting(), reportSvc+"/DeleteSchedule",
		scheduleActionReq{ID: made.Schedule.ID, TenantID: p.tenant,
			UpdatedBy: "e2e"}, p.opts()); err != nil {
		t.Fatalf("delete schedule: %v", err)
	}

	list, err := svcclient.Call[tenantReq, listSchedulesResp](
		context.Background(), p.reporting(), reportSvc+"/ListSchedules",
		tenantReq{TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	for _, s := range list.Schedules {
		if s.ID == made.Schedule.ID {
			t.Error("a deleted schedule is still listed, so it is still due to run")
		}
	}
}
