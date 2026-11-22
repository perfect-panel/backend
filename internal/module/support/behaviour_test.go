package support_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	userEntity "github.com/perfect-panel/backend/internal/module/identity/entity/user"
	taskEntity "github.com/perfect-panel/backend/internal/module/platform/entity/task"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	ticketEntity "github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/internal/module/support/internal/supporttest"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// The tests in this file run the support facade against its own repository
// implementations on the harness database, with Redis (the repositories'
// cache) on miniredis: they check what a caller observes, not which calls
// were made.

type supportWorld struct {
	*supporttest.Env
}

func openSupportWorld(t *testing.T) supportWorld {
	t.Helper()
	return supportWorld{Env: supporttest.New(t)}
}

// ───────────────────────── tickets ─────────────────────────

func (w supportWorld) ticketService(notify *fakeTicketNotifier) support.Service {
	deps := support.Deps{Tickets: w.Tickets}
	if notify != nil {
		deps.TicketNotify = notify
	}
	return support.New(deps)
}

func (w supportWorld) seedTicket(t *testing.T, userID int64, status uint8) int64 {
	t.Helper()
	row := &ticketEntity.Ticket{Title: "cannot connect", UserId: userID, Status: status}
	if err := w.DB.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	return row.Id
}

func (w supportWorld) ticket(t *testing.T, id int64) (*ticketEntity.Ticket, []ticketEntity.Follow) {
	t.Helper()
	var row ticketEntity.Ticket
	if err := w.DB.First(&row, id).Error; err != nil {
		t.Fatal(err)
	}
	var follows []ticketEntity.Follow
	if err := w.DB.Where("ticket_id = ?", id).Order("id").Find(&follows).Error; err != nil {
		t.Fatal(err)
	}
	return &row, follows
}

// A staff reply from the bot is the admin panel's reply: the follow is stored
// as staff text, the ticket waits for the user, and the reply is mirrored.
func TestStaffReplyRecordsFollowAndWaitsForUser(t *testing.T) {
	w := openSupportWorld(t)
	notify := &fakeTicketNotifier{}
	svc := w.ticketService(notify)
	id := w.seedTicket(t, 11, ticketEntity.Pending)

	result, err := svc.UpdateTicketAsStaff(context.Background(), &dto.StaffTicketUpdateCommand{TicketId: id, Reply: "请重启客户端", From: "admin"})
	if err != nil {
		t.Fatalf("UpdateTicketAsStaff: %v", err)
	}
	if result.PreviousStatus != ticketEntity.Pending {
		t.Fatalf("previous status = %d, want Pending", result.PreviousStatus)
	}
	row, follows := w.ticket(t, id)
	if row.Status != ticketEntity.Waiting {
		t.Fatalf("status = %d, want Waiting", row.Status)
	}
	if len(follows) != 1 || follows[0].From != "admin" || follows[0].Type != ticketEntity.FollowText || follows[0].Content != "请重启客户端" {
		t.Fatalf("follows = %+v, want one staff text reply", follows)
	}
	if len(notify.replies) != 1 || notify.replies[0] != (ticketReply{ticketID: id, from: "admin", content: "请重启客户端"}) || len(notify.statuses) != 0 {
		t.Fatalf("mirrored replies = %+v, statuses = %+v, want the reply mirrored", notify.replies, notify.statuses)
	}
}

// A reply typed inside the ticket's Telegram topic is stored, but not echoed
// back into the topic that already shows it. (A closed topic takes no
// message in Telegram until it is reopened, which reopens the ticket first;
// the ticket here awaits staff.)
func TestStaffReplyFromMirrorIsNotEchoed(t *testing.T) {
	w := openSupportWorld(t)
	notify := &fakeTicketNotifier{}
	svc := w.ticketService(notify)
	id := w.seedTicket(t, 11, ticketEntity.Pending)

	if _, err := svc.UpdateTicketAsStaff(context.Background(), &dto.StaffTicketUpdateCommand{TicketId: id, Reply: "reopened", From: "admin", FromMirror: true}); err != nil {
		t.Fatalf("UpdateTicketAsStaff: %v", err)
	}
	row, follows := w.ticket(t, id)
	if row.Status != ticketEntity.Waiting || len(follows) != 1 {
		t.Fatalf("status = %d, follows = %+v, want the reply stored", row.Status, follows)
	}
	if len(notify.replies) != 0 || len(notify.statuses) != 0 {
		t.Fatalf("mirror called: replies = %+v, statuses = %+v", notify.replies, notify.statuses)
	}
}

func TestStaffStatusChangeIsMirroredUnlessFromMirror(t *testing.T) {
	for _, fromMirror := range []bool{false, true} {
		w := openSupportWorld(t)
		notify := &fakeTicketNotifier{}
		svc := w.ticketService(notify)
		id := w.seedTicket(t, 11, ticketEntity.Waiting)

		result, err := svc.UpdateTicketAsStaff(context.Background(), &dto.StaffTicketUpdateCommand{TicketId: id, Status: ticketEntity.Closed, FromMirror: fromMirror})
		if err != nil {
			t.Fatalf("fromMirror %v: %v", fromMirror, err)
		}
		row, follows := w.ticket(t, id)
		if row.Status != ticketEntity.Closed || len(follows) != 0 || result.PreviousStatus != ticketEntity.Waiting {
			t.Fatalf("fromMirror %v: status = %d, follows = %+v, previous = %d", fromMirror, row.Status, follows, result.PreviousStatus)
		}
		mirrored := len(notify.statuses) == 1 && notify.statuses[0] == (ticketStatusMirror{ticketID: id, status: ticketEntity.Closed})
		if mirrored == fromMirror || len(notify.replies) != 0 {
			t.Fatalf("fromMirror %v: mirrored statuses = %+v", fromMirror, notify.statuses)
		}
	}
}

func TestStaffUpdateRejectsUnknownStatusAndMissingTicket(t *testing.T) {
	w := openSupportWorld(t)
	notify := &fakeTicketNotifier{}
	svc := w.ticketService(notify)
	id := w.seedTicket(t, 11, ticketEntity.Pending)

	for _, status := range []uint8{0, 9} {
		_, err := svc.UpdateTicketAsStaff(context.Background(), &dto.StaffTicketUpdateCommand{TicketId: id, Status: status})
		if xerr.CodeOf(err) != xerr.InvalidParams {
			t.Fatalf("status %d: error = %v, want invalid params", status, err)
		}
	}
	_, err := svc.UpdateTicketAsStaff(context.Background(), &dto.StaffTicketUpdateCommand{TicketId: id + 100, Reply: "hello", From: "admin"})
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing ticket error = %v, want gorm.ErrRecordNotFound in the chain", err)
	}
	if row, follows := w.ticket(t, id); row.Status != ticketEntity.Pending || len(follows) != 0 || len(notify.replies)+len(notify.statuses) != 0 {
		t.Fatalf("a rejected update changed state: status = %d, follows = %+v", row.Status, follows)
	}
}

// The admin panel, the user and the bot share one thread: every reply lands
// in it, and whoever wrote last decides who the ticket waits for.
func TestTicketConversationAcrossChannels(t *testing.T) {
	w := openSupportWorld(t)
	svc := w.ticketService(nil)
	ctx := ctxWithUser(11)
	if err := svc.CreateUserTicket(ctx, &dto.CreateUserTicketRequest{Title: "cannot connect", Description: "since today"}); err != nil {
		t.Fatalf("CreateUserTicket: %v", err)
	}
	list, err := svc.GetUserTicketList(ctx, &dto.GetUserTicketListRequest{Page: 1, Size: 10})
	if err != nil || list.Total != 1 {
		t.Fatalf("user tickets = %+v (err %v), want the new ticket", list, err)
	}
	id := list.List[0].Id

	steps := []struct {
		name string
		run  func() error
		want uint8
	}{
		{"admin panel reply", func() error {
			return svc.CreateTicketFollow(context.Background(), &dto.CreateTicketFollowRequest{TicketId: id, From: "System", Type: ticketEntity.FollowText, Content: "which client?"})
		}, ticketEntity.Waiting},
		{"user reply", func() error {
			return svc.CreateUserTicketFollow(ctx, &dto.CreateUserTicketFollowRequest{TicketId: id, Content: "the iOS one"})
		}, ticketEntity.Pending},
		{"bot reply", func() error {
			_, err := svc.UpdateTicketAsStaff(context.Background(), &dto.StaffTicketUpdateCommand{TicketId: id, Reply: "fixed", From: "admin"})
			return err
		}, ticketEntity.Waiting},
		{"user closes", func() error {
			return svc.UpdateUserTicketStatus(ctx, &dto.UpdateUserTicketStatusRequest{Id: id, Status: ptr(uint8(ticketEntity.Closed))})
		}, ticketEntity.Closed},
	}
	for _, step := range steps {
		if err := step.run(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if row, _ := w.ticket(t, id); row.Status != step.want {
			t.Fatalf("after %s: status = %d, want %d", step.name, row.Status, step.want)
		}
	}

	detail, err := svc.GetUserTicketDetails(ctx, &dto.GetUserTicketDetailRequest{Id: id})
	if err != nil {
		t.Fatalf("GetUserTicketDetails: %v", err)
	}
	var authors []string
	for _, f := range detail.Follows {
		authors = append(authors, f.From+":"+f.Content)
	}
	want := []string{"System:which client?", "User:the iOS one", "admin:fixed"}
	if !slices.Equal(authors, want) {
		t.Fatalf("thread = %v, want %v", authors, want)
	}
}

// Another user's ticket is neither readable nor writable, and a refused write
// leaves the ticket as it was.
func TestUserTicketOwnershipAgainstStoredTickets(t *testing.T) {
	w := openSupportWorld(t)
	svc := w.ticketService(nil)
	id := w.seedTicket(t, 99, ticketEntity.Waiting)
	stranger := ctxWithUser(11)

	if _, err := svc.GetUserTicketDetails(stranger, &dto.GetUserTicketDetailRequest{Id: id}); xerr.CodeOf(err) != xerr.InvalidAccess {
		t.Fatalf("read error = %v, want invalid access", err)
	}
	if err := svc.CreateUserTicketFollow(stranger, &dto.CreateUserTicketFollowRequest{TicketId: id, Content: "hi"}); xerr.CodeOf(err) != xerr.InvalidAccess {
		t.Fatalf("reply error = %v, want invalid access", err)
	}
	if err := svc.UpdateUserTicketStatus(stranger, &dto.UpdateUserTicketStatusRequest{Id: id, Status: ptr(uint8(ticketEntity.Closed))}); xerr.CodeOf(err) != xerr.InvalidAccess {
		t.Fatalf("close error = %v, want invalid access", err)
	}
	if row, follows := w.ticket(t, id); row.Status != ticketEntity.Waiting || len(follows) != 0 {
		t.Fatalf("status = %d, follows = %+v, want the foreign ticket untouched", row.Status, follows)
	}
	list, err := svc.GetUserTicketList(stranger, &dto.GetUserTicketListRequest{Page: 1, Size: 10})
	if err != nil || list.Total != 0 {
		t.Fatalf("stranger's list = %+v (err %v), want nothing of the other user's", list, err)
	}
}

func TestAdminTicketListFiltersByStatus(t *testing.T) {
	w := openSupportWorld(t)
	svc := w.ticketService(nil)
	w.seedTicket(t, 1, ticketEntity.Pending)
	w.seedTicket(t, 2, ticketEntity.Closed)

	// The desk hides closed tickets unless asked for them.
	open, err := svc.GetTicketList(context.Background(), &dto.GetTicketListRequest{Page: 1, Size: 10})
	if err != nil || open.Total != 1 || open.List[0].UserId != 1 {
		t.Fatalf("default list = %+v (err %v), want only the open ticket", open, err)
	}
	closed, err := svc.GetTicketList(context.Background(), &dto.GetTicketListRequest{Page: 1, Size: 10, Status: ptr(uint8(ticketEntity.Closed))})
	if err != nil || closed.Total != 1 || closed.List[0].UserId != 2 {
		t.Fatalf("closed list = %+v (err %v), want the closed ticket", closed, err)
	}
	pending, err := svc.GetTicketList(context.Background(), &dto.GetTicketListRequest{Page: 1, Size: 10, Status: ptr(uint8(ticketEntity.Pending))})
	if err != nil || pending.Total != 1 || pending.List[0].UserId != 1 {
		t.Fatalf("pending tickets = %+v (err %v)", pending, err)
	}
	if err := svc.UpdateTicketStatus(context.Background(), &dto.UpdateTicketStatusRequest{Id: pending.List[0].Id, Status: ptr(uint8(ticketEntity.Processed))}); err != nil {
		t.Fatalf("UpdateTicketStatus: %v", err)
	}
	detail, err := svc.GetTicket(context.Background(), &dto.GetTicketRequest{Id: pending.List[0].Id})
	if err != nil || detail.Status != ticketEntity.Processed {
		t.Fatalf("detail = %+v (err %v), want Processed", detail, err)
	}
}

// ───────────────────────── announcements ─────────────────────────

func TestAnnouncementsVisibleOnlyOnceShown(t *testing.T) {
	w := openSupportWorld(t)
	svc := support.New(support.Deps{Announcements: w.Announcements})
	ctx := context.Background()

	for _, title := range []string{"maintenance", "new plans"} {
		if err := svc.CreateAnnouncement(ctx, &dto.CreateAnnouncementRequest{Title: title, Content: title + " body"}); err != nil {
			t.Fatalf("CreateAnnouncement: %v", err)
		}
	}
	admin, err := svc.GetAnnouncementList(ctx, &dto.GetAnnouncementListRequest{Page: 1, Size: 10})
	if err != nil || admin.Total != 2 {
		t.Fatalf("admin list = %+v (err %v), want both drafts", admin, err)
	}
	public, err := svc.QueryAnnouncement(ctx, &dto.QueryAnnouncementRequest{Page: 1, Size: 10})
	if err != nil || public.Total != 0 {
		t.Fatalf("public list = %+v (err %v), want no unpublished announcement", public, err)
	}

	var draft dto.Announcement
	for _, item := range admin.List {
		if item.Title == "maintenance" {
			draft = item
		}
	}
	if err := svc.UpdateAnnouncement(ctx, &dto.UpdateAnnouncementRequest{Id: draft.Id, Title: "maintenance", Content: "tonight", Show: ptr(true), Pinned: ptr(true)}); err != nil {
		t.Fatalf("UpdateAnnouncement: %v", err)
	}
	public, err = svc.QueryAnnouncement(ctx, &dto.QueryAnnouncementRequest{Page: 1, Size: 10})
	if err != nil || public.Total != 1 || public.List[0].Content != "tonight" || !*public.List[0].Pinned {
		t.Fatalf("public list = %+v (err %v), want the published announcement", public, err)
	}
	// An update that omits the flags keeps them.
	if err := svc.UpdateAnnouncement(ctx, &dto.UpdateAnnouncementRequest{Id: draft.Id, Title: "maintenance", Content: "moved"}); err != nil {
		t.Fatalf("UpdateAnnouncement: %v", err)
	}
	got, err := svc.GetAnnouncement(ctx, &dto.GetAnnouncementRequest{Id: draft.Id})
	if err != nil || got.Content != "moved" || !*got.Show || !*got.Pinned {
		t.Fatalf("announcement = %+v (err %v), want the flags kept", got, err)
	}
	if err := svc.DeleteAnnouncement(ctx, &dto.DeleteAnnouncementRequest{Id: draft.Id}); err != nil {
		t.Fatalf("DeleteAnnouncement: %v", err)
	}
	if _, err := svc.GetAnnouncement(ctx, &dto.GetAnnouncementRequest{Id: draft.Id}); xerr.CodeOf(err) != xerr.DatabaseQueryError {
		t.Fatalf("deleted announcement error = %v", err)
	}
}

// ───────────────────────── documents ─────────────────────────

func TestDocumentsLifecycle(t *testing.T) {
	w := openSupportWorld(t)
	svc := support.New(support.Deps{Documents: w.Documents, Subscriptions: fakeSubscriptionReader{active: true}})
	ctx := context.Background()

	create := func(title string, show bool) {
		t.Helper()
		if err := svc.CreateDocument(ctx, &dto.CreateDocumentRequest{Title: title, Content: gatedContent, Tags: []string{"setup", "ios", "setup"}, Show: ptr(show)}); err != nil {
			t.Fatalf("CreateDocument: %v", err)
		}
	}
	create("iOS guide", true)
	create("internal notes", false)

	admin, err := svc.GetDocumentList(ctx, &dto.GetDocumentListRequest{Page: 1, Size: 10})
	if err != nil || admin.Total != 2 {
		t.Fatalf("admin list = %+v (err %v), want both documents", admin, err)
	}
	ids := map[string]int64{}
	for _, doc := range admin.List {
		ids[doc.Title] = doc.Id
		if !slices.Equal(doc.Tags, []string{"setup", "ios"}) {
			t.Fatalf("tags = %v, want the de-duplicated tags", doc.Tags)
		}
	}

	subscriber := ctxWithUser(9)
	doc, err := svc.QueryDocumentDetail(subscriber, &dto.QueryDocumentDetailRequest{Id: ids["iOS guide"]})
	if err != nil || !strings.Contains(doc.Content, "secret") || strings.Contains(doc.Content, "{{") {
		t.Fatalf("document = %+v (err %v), want the gated content rendered", doc, err)
	}
	// A hidden document answers like a missing one on the user side, and
	// is still there for the admin.
	if _, err := svc.QueryDocumentDetail(subscriber, &dto.QueryDocumentDetailRequest{Id: ids["internal notes"]}); xerr.CodeOf(err) != xerr.DatabaseQueryError {
		t.Fatalf("hidden document error = %v, want the not-found answer", err)
	}
	if detail, err := svc.GetDocumentDetail(ctx, &dto.GetDocumentDetailRequest{Id: ids["internal notes"]}); err != nil || detail.Title != "internal notes" {
		t.Fatalf("admin detail = %+v (err %v)", detail, err)
	}

	if err := svc.UpdateDocument(ctx, &dto.UpdateDocumentRequest{Id: ids["iOS guide"], Title: "iOS guide v2", Content: "plain", Tags: []string{"ios"}, Show: ptr(true)}); err != nil {
		t.Fatalf("UpdateDocument: %v", err)
	}
	if doc, err := svc.QueryDocumentDetail(subscriber, &dto.QueryDocumentDetailRequest{Id: ids["iOS guide"]}); err != nil || doc.Title != "iOS guide v2" || doc.Content != "plain" {
		t.Fatalf("updated document = %+v (err %v)", doc, err)
	}
	if err := svc.BatchDeleteDocument(ctx, &dto.BatchDeleteDocumentRequest{Ids: []int64{ids["iOS guide"], ids["internal notes"]}}); err != nil {
		t.Fatalf("BatchDeleteDocument: %v", err)
	}
	if list, err := svc.GetDocumentList(ctx, &dto.GetDocumentListRequest{Page: 1, Size: 10}); err != nil || list.Total != 0 {
		t.Fatalf("list after delete = %+v (err %v)", list, err)
	}
}

// ───────────────────────── ads ─────────────────────────

func TestPublicAdsFollowScheduleAndStatus(t *testing.T) {
	w := openSupportWorld(t)
	svc := support.New(support.Deps{Ads: w.Ads})
	ctx := context.Background()
	now := time.Now()

	for _, ad := range []dto.CreateAdsRequest{
		{Title: "running", Status: 1, StartTime: now.Add(-time.Hour).UnixMilli(), EndTime: now.Add(time.Hour).UnixMilli()},
		{Title: "upcoming", Status: 1, StartTime: now.Add(time.Hour).UnixMilli(), EndTime: now.Add(2 * time.Hour).UnixMilli()},
		{Title: "disabled", Status: 0, StartTime: now.Add(-time.Hour).UnixMilli(), EndTime: now.Add(time.Hour).UnixMilli()},
	} {
		if err := svc.CreateAds(ctx, &ad); err != nil {
			t.Fatalf("CreateAds: %v", err)
		}
	}
	titles := func(list []dto.Ads) []string {
		var out []string
		for _, ad := range list {
			out = append(out, ad.Title)
		}
		slices.Sort(out)
		return out
	}
	public, err := svc.GetPublicAds(ctx, &dto.GetAdsRequest{})
	if err != nil || !slices.Equal(titles(public.List), []string{"running"}) {
		t.Fatalf("public ads = %v (err %v), want only the running ad", titles(public.List), err)
	}
	admin, err := svc.GetAdsList(ctx, &dto.GetAdsListRequest{Page: 1, Size: 10})
	if err != nil || admin.Total != 3 {
		t.Fatalf("admin ads = %+v (err %v), want every ad", admin, err)
	}

	var upcoming dto.Ads
	for _, ad := range admin.List {
		if ad.Title == "upcoming" {
			upcoming = ad
		}
	}
	if err := svc.UpdateAds(ctx, &dto.UpdateAdsRequest{Id: int64(upcoming.Id), Title: "upcoming", Status: 1,
		StartTime: now.Add(-time.Minute).UnixMilli(), EndTime: now.Add(time.Hour).UnixMilli()}); err != nil {
		t.Fatalf("UpdateAds: %v", err)
	}
	if public, err := svc.GetPublicAds(ctx, &dto.GetAdsRequest{}); err != nil || !slices.Equal(titles(public.List), []string{"running", "upcoming"}) {
		t.Fatalf("public ads = %v (err %v), want the rescheduled ad live", titles(public.List), err)
	}
	if err := svc.DeleteAds(ctx, &dto.DeleteAdsRequest{Id: int64(upcoming.Id)}); err != nil {
		t.Fatalf("DeleteAds: %v", err)
	}
	if _, err := svc.GetAdsDetail(ctx, &dto.GetAdsDetailRequest{Id: int64(upcoming.Id)}); err == nil {
		t.Fatal("a deleted ad is still readable")
	}
}

// ───────────────────────── marketing ─────────────────────────

type fixedRecipients []string

func (r fixedRecipients) QueryEmailRecipients(context.Context, *userEntity.EmailRecipientFilter) ([]string, error) {
	return r, nil
}

func (r fixedRecipients) CountEmailRecipients(context.Context, *userEntity.EmailRecipientFilter) (int64, error) {
	return int64(len(r)), nil
}

type fixedSelector []int64

func (s fixedSelector) QuerySubscribeIdsByFilter(context.Context, *usersub.SubscribeFilter) ([]int64, error) {
	return s, nil
}

func (s fixedSelector) CountSubscribesByFilter(context.Context, *usersub.SubscribeFilter) (int64, error) {
	return int64(len(s)), nil
}

type recordingQueue struct {
	emails, quotas []int64
	fail           error
}

func (q *recordingQueue) EnqueueBatchEmail(_ context.Context, taskID int64, _ time.Time) (string, error) {
	if q.fail != nil {
		return "", q.fail
	}
	q.emails = append(q.emails, taskID)
	return fmt.Sprintf("marketing-email-%d-initial", taskID), nil
}

func (q *recordingQueue) EnqueueQuota(_ context.Context, taskID int64) error {
	if q.fail != nil {
		return q.fail
	}
	q.quotas = append(q.quotas, taskID)
	return nil
}

type recordingStopper struct{ stopped []int64 }

func (s *recordingStopper) StopBatchEmail(taskID int64) { s.stopped = append(s.stopped, taskID) }

func (w supportWorld) marketing(queue *recordingQueue, stopper *recordingStopper) support.Service {
	return support.New(support.Deps{
		Tasks:        w.Tasks,
		Recipients:   fixedRecipients{"a@example.com", "b@example.com", "a@example.com"},
		QuotaTargets: fixedSelector{3, 5},
		Queue:        queue,
		EmailStopper: stopper,
	})
}

func TestBatchEmailCampaignLifecycle(t *testing.T) {
	w := openSupportWorld(t)
	queue := &recordingQueue{}
	stopper := &recordingStopper{}
	svc := w.marketing(queue, stopper)
	ctx := context.Background()

	count, err := svc.GetPreSendEmailCount(ctx, &dto.GetPreSendEmailCountRequest{Scope: taskEntity.ScopeAll.Int8(), Additional: "B@example.com\nc@example.com"})
	if err != nil || count.Count != 3 {
		t.Fatalf("pre-send count = %+v (err %v), want the de-duplicated recipients", count, err)
	}
	if err := svc.CreateBatchSendEmailTask(ctx, &dto.CreateBatchSendEmailTaskRequest{
		Subject: "news", Content: "<p>hi</p>", Scope: taskEntity.ScopeAll.Int8(), Additional: "c@example.com",
	}); err != nil {
		t.Fatalf("CreateBatchSendEmailTask: %v", err)
	}
	if len(queue.emails) != 1 {
		t.Fatalf("enqueued = %v, want the campaign scheduled", queue.emails)
	}
	id := queue.emails[0]

	list, err := svc.GetBatchSendEmailTaskList(ctx, &dto.GetBatchSendEmailTaskListRequest{})
	if err != nil || list.Total != 1 {
		t.Fatalf("campaigns = %+v (err %v)", list, err)
	}
	campaign := list.List[0]
	if campaign.Id != id || campaign.Subject != "news" || campaign.Total != 3 || campaign.Additional != "c@example.com" || campaign.Status != uint8(taskEntity.StatusPending) {
		t.Fatalf("campaign = %+v, want the stored campaign with its de-duplicated total", campaign)
	}

	if err := svc.StopBatchSendEmailTask(ctx, &dto.StopBatchSendEmailTaskRequest{Id: id}); err != nil {
		t.Fatalf("StopBatchSendEmailTask: %v", err)
	}
	status, err := svc.GetBatchSendEmailTaskStatus(ctx, &dto.GetBatchSendEmailTaskStatusRequest{Id: id})
	if err != nil || status.Status != uint8(taskEntity.StatusCancelled) || !slices.Equal(stopper.stopped, []int64{id}) {
		t.Fatalf("status = %+v (err %v), stopped = %v, want the campaign cancelled", status, err, stopper.stopped)
	}
	if err := svc.StopBatchSendEmailTask(ctx, &dto.StopBatchSendEmailTaskRequest{Id: id}); err == nil {
		t.Fatal("a cancelled campaign was stopped again")
	}
}

// A campaign the queue refused is recorded as such rather than left pending
// forever.
func TestBatchEmailEnqueueFailureIsRecorded(t *testing.T) {
	w := openSupportWorld(t)
	svc := w.marketing(&recordingQueue{fail: errors.New("redis down")}, &recordingStopper{})
	ctx := context.Background()

	err := svc.CreateBatchSendEmailTask(ctx, &dto.CreateBatchSendEmailTaskRequest{Subject: "news", Content: "hi", Scope: taskEntity.ScopeAll.Int8()})
	if xerr.CodeOf(err) != xerr.QueueEnqueueError {
		t.Fatalf("error = %v, want the enqueue failure", err)
	}
	list, err := svc.GetBatchSendEmailTaskList(ctx, &dto.GetBatchSendEmailTaskListRequest{})
	if err != nil || list.Total != 1 || list.List[0].Status != uint8(taskEntity.StatusEnqueueFailed) || !strings.Contains(list.List[0].Errors, "redis down") {
		t.Fatalf("campaigns = %+v (err %v), want it marked enqueue-failed", list, err)
	}
}

func TestQuotaTaskLifecycle(t *testing.T) {
	w := openSupportWorld(t)
	queue := &recordingQueue{}
	svc := w.marketing(queue, &recordingStopper{})
	ctx := context.Background()

	count, err := svc.QueryQuotaTaskPreCount(ctx, &dto.QueryQuotaTaskPreCountRequest{})
	if err != nil || count.Count != 2 {
		t.Fatalf("pre-count = %+v (err %v)", count, err)
	}
	if err := svc.CreateQuotaTask(ctx, &dto.CreateQuotaTaskRequest{Days: 7}); err != nil {
		t.Fatalf("CreateQuotaTask: %v", err)
	}
	if len(queue.quotas) != 1 {
		t.Fatalf("enqueued = %v", queue.quotas)
	}
	list, err := svc.QueryQuotaTaskList(ctx, &dto.QueryQuotaTaskListRequest{})
	if err != nil || list.Total != 1 {
		t.Fatalf("quota tasks = %+v (err %v)", list, err)
	}
	if got := list.List[0]; got.Days != 7 || !slices.Equal(got.Objects, []int64{3, 5}) || got.Total != 2 {
		t.Fatalf("quota task = %+v, want 7 days for both subscriptions", got)
	}
	if got := list.List[0]; got.Id != queue.quotas[0] || got.Status != uint8(taskEntity.StatusPending) {
		t.Fatalf("quota task = %+v, want the enqueued task pending", got)
	}
	if err := svc.CreateQuotaTask(ctx, &dto.CreateQuotaTaskRequest{}); err == nil {
		t.Fatal("a quota task without any action was accepted")
	}
}
