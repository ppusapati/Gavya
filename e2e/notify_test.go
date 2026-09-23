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
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

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
	Timezone   string `json:"timezone"`
	CreatedBy  string `json:"created_by"`
}

type scheduleResp struct {
	Schedule *struct {
		ID         string `json:"id"`
		ReportType string `json:"report_type"`
		IsActive   bool   `json:"is_active"`
		Timezone   string `json:"timezone"`
		NextRunAt  string `json:"next_run_at"`
	} `json:"schedule"`
}

type reportKindsResp struct {
	Kinds []struct {
		Name        string   `json:"name"`
		Summary     string   `json:"summary"`
		Needs       []string `json:"needs"`
		Schedulable bool     `json:"schedulable"`
	} `json:"kinds"`
}

type reportContentResp struct {
	Content     []byte `json:"content"`
	ContentType string `json:"content_type"`
	Filename    string `json:"filename"`
	Rows        int64  `json:"row_count"`
	Truncated   bool   `json:"truncated"`
	Bytes       int64  `json:"bytes"`
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
			ReportType: "collections", Parameters: `{"from":"2026-02-01","to":"2026-02-15"}`,
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
			Schedule: "0 6 * * *", Parameters: `{"window":"yesterday"}`,
			Timezone: "Asia/Kolkata", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	if !made.Schedule.IsActive {
		t.Error("a new schedule is inactive; one that never runs is one nobody notices " +
			"is not running")
	}
	// The first firing is worked out when the schedule is written, not left for
	// a sweep to adopt. A schedule whose next run is a blank is one nobody can
	// check before the morning it does not arrive.
	if made.Schedule.NextRunAt == "" {
		t.Error("a new schedule has no next firing")
	}
	if made.Schedule.Timezone != "Asia/Kolkata" {
		t.Errorf("the schedule's zone reads back as %q; six in the morning is six where the "+
			"society is", made.Schedule.Timezone)
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

// The catalogue is served, and it is what a request is checked against.
//
// A client holding its own list is one that offers a type the platform stopped
// producing, or hides one it started. This is the list the runner reads.
func TestTheReportCatalogueIsServedAndIsWhatRequestsAreCheckedAgainst(t *testing.T) {
	p := startPlatform(t)

	kinds, err := svcclient.Call[struct{}, reportKindsResp](
		context.Background(), p.reporting(), reportSvc+"/ListReportKinds",
		struct{}{}, p.opts())
	if err != nil {
		t.Fatalf("list report kinds: %v", err)
	}
	if len(kinds.Kinds) == 0 {
		t.Fatal("the platform serves an empty catalogue, so nothing can be asked for")
	}

	var schedulable, oneAtATime int
	for _, k := range kinds.Kinds {
		if k.Summary == "" {
			t.Errorf("%s has no summary, so nobody choosing between these can tell what it is",
				k.Name)
		}
		if k.Schedulable {
			schedulable++
		} else {
			oneAtATime++
		}
	}
	if schedulable == 0 {
		t.Error("nothing in the catalogue can be scheduled, so the schedule runner has " +
			"nothing it could ever fire")
	}
	if oneAtATime == 0 {
		t.Error("everything in the catalogue is schedulable; a type that names a cycle is " +
			"not, and if none is, that distinction is not being enforced")
	}

	// A type nobody wrote is refused when it is asked for, and the refusal
	// names what there is. An empty file marked complete is the failure this
	// prevents: nothing errors, and a person reads a period in which nothing
	// happened.
	_, err = svcclient.Call[requestReportReq, reportResp](
		context.Background(), p.reporting(), reportSvc+"/RequestReport",
		requestReportReq{TenantID: p.tenant, Name: "made up", ReportType: "daily_yield",
			Parameters: `{"from":"2026-02-01","to":"2026-02-02"}`, FileFormat: "csv",
			RequestedBy: "e2e", CreatedBy: "e2e"}, p.opts())
	if err == nil {
		t.Fatal("a report type nobody wrote was accepted")
	}
	if !strings.Contains(err.Error(), "collections") {
		t.Errorf("the refusal does not say what the platform does produce: %v", err)
	}

	// And a schedule cannot ask for a type that names a cycle. Refused when the
	// schedule is written rather than at six in the morning, which is the
	// difference between an error message and a report that never arrives.
	_, err = svcclient.Call[createScheduleReq, scheduleResp](
		context.Background(), p.reporting(), reportSvc+"/CreateSchedule",
		createScheduleReq{TenantID: p.tenant, ReportType: "settlement_summary",
			Schedule: "0 6 * * *", Parameters: `{"window":"yesterday"}`,
			Timezone: "Asia/Kolkata", CreatedBy: "e2e"}, p.opts())
	if err == nil {
		t.Error("a schedule was written for a report that names a cycle, which a schedule " +
			"firing at six in the morning has no way to choose")
	}
}

// A requested report is actually produced, and the bytes come back.
//
// This is the whole point of the runner. Before it existed, RequestReport wrote
// a row with status 'pending' and nothing in the platform ever moved it: the
// console had to say so on the page, because a Generate button with a spinner
// would have been a control that reports success while doing nothing.
//
// The report here is a collections report over a period this test's tenant has
// no milk in, so it is empty — and that is the point worth being careful about.
// An empty report and a report whose source could not be reached look identical
// on a screen, so this asserts the status and the header rather than the row
// count: completed with a header is "nobody delivered any milk", and failed
// with a reason is everything else.
func TestARequestedReportIsProducedAndHandedOver(t *testing.T) {
	p := startPlatform(t)

	made, err := svcclient.Call[requestReportReq, reportResp](
		context.Background(), p.reporting(), reportSvc+"/RequestReport",
		requestReportReq{TenantID: p.tenant, Name: "a fortnight",
			ReportType: "collections", Parameters: `{"from":"2026-02-01","to":"2026-02-15"}`,
			FileFormat: "csv", RequestedBy: "e2e", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("request report: %v", err)
	}

	// The runner claims within its sweep interval and a request kicks a sweep,
	// so this is normally immediate. Polled rather than slept on, because a
	// fixed wait is either flaky or slow and this is neither.
	var final string
	var reason string
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		got, err := svcclient.Call[idTenantReq, reportResp](
			context.Background(), p.reporting(), reportSvc+"/GetReport",
			idTenantReq{ID: made.Report.ID, TenantID: p.tenant}, p.opts())
		if err != nil {
			t.Fatalf("get report: %v", err)
		}
		final = got.Report.Status
		if final == "completed" || final == "failed" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	if final != "completed" {
		// The reason is fetched from the listing, which carries it, so a
		// failure here says what went wrong rather than only that it did.
		list, lerr := svcclient.Call[tenantReq, listReportsResp](
			context.Background(), p.reporting(), reportSvc+"/ListReports",
			tenantReq{TenantID: p.tenant}, p.opts())
		if lerr == nil {
			for _, r := range list.Reports {
				if r.ID == made.Report.ID {
					reason = r.FailureReason
				}
			}
		}
		t.Fatalf("the report ended %q rather than completed: %s\n"+
			"Nothing moved it at all would show as 'pending', which is the state this "+
			"whole runner was written to leave.", final, reason)
	}

	// And the bytes come back.
	content, err := svcclient.Call[idTenantReq, reportContentResp](
		context.Background(), p.reporting(), reportSvc+"/GetReportContent",
		idTenantReq{ID: made.Report.ID, TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("get report content: %v", err)
	}
	if len(content.Content) == 0 {
		t.Fatal("a completed report handed over nothing; even an empty period has a header")
	}
	if int64(len(content.Content)) != content.Bytes {
		t.Errorf("the report says it is %d bytes and %d arrived",
			content.Bytes, len(content.Content))
	}
	if !strings.HasPrefix(content.ContentType, "text/csv") {
		t.Errorf("content type is %q", content.ContentType)
	}
	if !strings.Contains(content.Filename, "collections") ||
		!strings.HasSuffix(content.Filename, ".csv") {
		t.Errorf("filename is %q; it is built by the service rather than from the report's "+
			"name, which is free text", content.Filename)
	}
	// The header names the columns, so a period with no milk in it is still a
	// file somebody can open rather than an empty one they cannot read.
	head := string(content.Content)
	for _, col := range []string{"collected_on", "producer_ref", "amount"} {
		if !strings.Contains(head, col) {
			t.Errorf("the report has no %s column:\n%s", col, head)
		}
	}

	// A download is now offered, because there is something to download.
	if _, err := svcclient.Call[idTenantReq, downloadURLResp](
		context.Background(), p.reporting(), reportSvc+"/GetReportDownloadURL",
		idTenantReq{ID: made.Report.ID, TenantID: p.tenant}, p.opts()); err != nil {
		t.Errorf("a completed report offers no locator: %v", err)
	}

	// And a second tenant cannot read the content, which is the tenant's own
	// collections rather than a file about them.
	other := newID("tnt")
	if _, err := svcclient.Call[idTenantReq, reportContentResp](
		context.Background(), p.reporting(), reportSvc+"/GetReportContent",
		idTenantReq{ID: made.Report.ID, TenantID: other},
		actingAs(other, "e2e")); err == nil {
		t.Error("a second tenant read the contents of a report it did not request")
	}
}

// A signed link fetches the report, and nothing else does.
//
// This is the property the whole arrangement exists for: a browser following an
// <a href> sends no Authorization header, and neither does curl or whoever the
// link was forwarded to. So the link carries its own authority — and the same
// request without a valid one has to get nothing.
//
// The link is fetched with a plain HTTP client rather than the Connect one,
// deliberately. A test that fetched it through the platform's own client would
// be proving something about the client; what matters is that anything at all
// can follow it.
func TestASignedLinkFetchesTheReportAndNothingElseDoes(t *testing.T) {
	p := startPlatform(t)

	made, err := svcclient.Call[requestReportReq, reportResp](
		context.Background(), p.reporting(), reportSvc+"/RequestReport",
		requestReportReq{TenantID: p.tenant, Name: "a fortnight",
			ReportType: "collections", Parameters: `{"from":"2026-02-01","to":"2026-02-15"}`,
			FileFormat: "csv", RequestedBy: "e2e", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("request report: %v", err)
	}
	waitForReport(t, p, made.Report.ID, "completed")

	link, err := svcclient.Call[idTenantReq, downloadURLResp](
		context.Background(), p.reporting(), reportSvc+"/GetReportDownloadURL",
		idTenantReq{ID: made.Report.ID, TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("get download link: %v", err)
	}
	if !strings.HasPrefix(link.URL, "/download/report?t=") {
		t.Fatalf("the link is %q; with no public base configured it should be root-relative "+
			"so a console can resolve it against the gateway it is already talking to", link.URL)
	}

	base := p.baseURLs["reporting-service"]

	// The link, followed by something that knows nothing about this platform.
	res, err := http.Get(base + link.URL)
	if err != nil {
		t.Fatalf("follow the link: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("following the link gave %d: %s", res.StatusCode, body)
	}

	// The headers that stop a tenant's own text becoming script on this
	// platform's origin.
	if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options is %q", got)
	}
	if got := res.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Errorf("Content-Disposition is %q, which does not force a download", got)
	}
	if got := res.Header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("Cache-Control is %q; a link is a bearer credential and a shared cache "+
			"would serve the answer after the link stopped working", got)
	}

	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "collected_on") {
		t.Errorf("the report did not come back:\n%s", body)
	}

	// And now the other half. Each of these is a way somebody might try to turn
	// one link into a key to the table, and every one of them has to fail.
	for _, tc := range []struct {
		name string
		url  string
		want int
	}{
		{"no token at all", base + "/download/report", http.StatusForbidden},
		{"an empty token", base + "/download/report?t=", http.StatusForbidden},
		{"a token that is not one", base + "/download/report?t=let-me-in", http.StatusForbidden},
		{"one character changed", base + tamper(link.URL), http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := http.Get(tc.url)
			if err != nil {
				t.Fatalf("%v", err)
			}
			defer res.Body.Close()
			body, _ := io.ReadAll(res.Body)
			if res.StatusCode != tc.want {
				t.Errorf("gave %d, want %d: %s", res.StatusCode, tc.want, body)
			}
			if strings.Contains(string(body), "collected_on") {
				t.Errorf("a request with %s was served the report", tc.name)
			}
		})
	}

	// A report that has not been produced has no link to give.
	pendingReport, err := svcclient.Call[requestReportReq, reportResp](
		context.Background(), p.reporting(), reportSvc+"/RequestReport",
		requestReportReq{TenantID: p.tenant, Name: "will fail",
			ReportType: "settlement_summary", Parameters: `{"cycle_id":"CYC_NOT_A_CYCLE"}`,
			FileFormat: "csv", RequestedBy: "e2e", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("request a report that cannot be produced: %v", err)
	}
	waitForReport(t, p, pendingReport.Report.ID, "failed")
	if _, err := svcclient.Call[idTenantReq, downloadURLResp](
		context.Background(), p.reporting(), reportSvc+"/GetReportDownloadURL",
		idTenantReq{ID: pendingReport.Report.ID, TenantID: p.tenant}, p.opts()); err == nil {
		t.Error("a link was issued for a report that produced nothing, so following it would " +
			"fail after somebody had sent it on")
	}
}

// tamper changes one character of the token in a link.
func tamper(link string) string {
	i := strings.Index(link, "t=")
	if i < 0 || i+2 >= len(link) {
		return link
	}
	b := []byte(link)
	// The last character of the token, which is inside the signature.
	if b[len(b)-1] == 'A' {
		b[len(b)-1] = 'B'
	} else {
		b[len(b)-1] = 'A'
	}
	return string(b)
}

// waitForReport polls until a report reaches a settled state.
func waitForReport(t *testing.T, p *platform, id, want string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		got, err := svcclient.Call[idTenantReq, reportResp](
			context.Background(), p.reporting(), reportSvc+"/GetReport",
			idTenantReq{ID: id, TenantID: p.tenant}, p.opts())
		if err != nil {
			t.Fatalf("get report: %v", err)
		}
		last = got.Report.Status
		if last == want {
			return
		}
		if last == "completed" || last == "failed" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("the report ended %q rather than %q", last, want)
}
