// Package supporttest is the support module's behaviour-test harness: the
// module's real repositories (tickets, announcements, ads and documents) and
// the platform's task bookkeeping the marketing tasks write, over a private
// in-memory SQLite database with miniredis as their cache, together with
// recording fakes of the module's ports onto the other domains, seeding and
// reading helpers, a way to make the database refuse statements, the API
// views of stored rows and a client for the Hertz handlers. Tests check what
// a flow stored and what a caller sees, not which repository call was made.
//
// The harness builds no service: the subdomain packages, the facade and the
// handler tests build theirs from Env, so any of them may import it. It also
// gives the handler tests, which may import no other module, what they need
// of the identity and platform modules (a signed-in user, the task statuses).
// Only tests import it.
package supporttest

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform"
	"github.com/perfect-panel/backend/internal/module/platform/entity/task"
	"github.com/perfect-panel/backend/internal/module/support/entity/ads"
	"github.com/perfect-panel/backend/internal/module/support/entity/announcement"
	"github.com/perfect-panel/backend/internal/module/support/entity/document"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/internal/module/support/internal/repo"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

// ClientIP, UserAgent and ActorID are the request metadata Context carries.
const (
	ClientIP  = "203.0.113.9"
	UserAgent = "support-test/1.0"
	ActorID   = 1
)

// The task statuses the marketing tasks move through, re-exported for the
// handler tests, which may not import the platform module.
const (
	TaskPending       = task.StatusPending
	TaskInProgress    = task.StatusInProgress
	TaskCancelled     = task.StatusCancelled
	TaskEnqueueFailed = task.StatusEnqueueFailed
)

// Env is one test's database, cache and repositories.
type Env struct {
	DB    *gorm.DB
	Redis *redis.Client
	Mini  *miniredis.Miniredis

	Tickets       repository.TicketRepo
	Announcements repository.AnnouncementRepo
	Ads           repository.AdsRepo
	Documents     repository.DocumentRepo
	// Tasks is the platform's task repository, which records the marketing
	// tasks.
	Tasks repository.TaskRepo
}

var databases atomic.Int64

// New opens a fresh database with the support tables and the task tables.
func New(t testing.TB) *Env {
	t.Helper()
	name := fmt.Sprintf("file:supporttest-%d?mode=memory&cache=shared", databases.Add(1))
	db, err := gorm.Open(sqlite.Open(name), &gorm.Config{
		TranslateError:                   true,
		IgnoreRelationshipsWhenMigrating: true,
		Logger:                           gormlog.Default.LogMode(gormlog.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// One connection: the shared-cache database lives as long as it is open,
	// and no statement waits on another connection's table lock.
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&ticket.Ticket{}, &ticket.Follow{}, &announcement.Announcement{},
		&ads.Ads{}, &document.Document{}, &task.Task{}, &task.TaskError{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	mini := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: mini.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = rds.Close() })
	conn := repository.ModuleConn{DB: db, Redis: rds}
	cached := conn.Conn()
	return &Env{
		DB:            db,
		Redis:         rds,
		Mini:          mini,
		Tickets:       repo.NewTicketRepo(cached),
		Announcements: repo.NewAnnouncementRepo(cached),
		Ads:           repo.NewAdsRepo(cached),
		Documents:     repo.NewDocumentRepo(cached),
		Tasks:         platform.NewRepoBuilder()(conn).Tasks,
	}
}

// WithUser returns ctx carrying the signed-in user id, as the auth middleware
// stores it for the user-facing endpoints.
func WithUser(ctx context.Context, id int64) context.Context {
	return user.NewContext(ctx, &user.User{Id: id})
}

// Context is a request context carrying ClientIP, UserAgent and ActorID, as
// the access-log and auth middlewares set them for an administrator's
// request.
func Context() context.Context {
	return WithMetadata(context.Background())
}

// WithMetadata returns ctx carrying the request metadata Context carries.
func WithMetadata(ctx context.Context) context.Context {
	return requestmeta.WithActor(requestmeta.With(ctx, requestmeta.New(ClientIP, UserAgent)), ActorID)
}

func (e *Env) create(t testing.TB, value any) {
	t.Helper()
	if err := e.DB.Create(value).Error; err != nil {
		t.Fatalf("seed %T: %v", value, err)
	}
}

func (e *Env) load(t testing.TB, value any, id int64) {
	t.Helper()
	if err := e.DB.First(value, id).Error; err != nil {
		t.Fatalf("load %T %d: %v", value, id, err)
	}
}

// Ticket stores a ticket; an unset title is "cannot connect" and an unset
// status Pending.
func (e *Env) Ticket(t testing.TB, row ticket.Ticket) *ticket.Ticket {
	t.Helper()
	if row.Title == "" {
		row.Title = "cannot connect"
	}
	if row.Status == 0 {
		row.Status = ticket.Pending
	}
	e.create(t, &row)
	return &row
}

// Follow stores a reply on a ticket; an unset type is text.
func (e *Env) Follow(t testing.TB, row ticket.Follow) *ticket.Follow {
	t.Helper()
	if row.Type == 0 {
		row.Type = ticket.FollowText
	}
	e.create(t, &row)
	return &row
}

// ReloadTicket reads a ticket as stored.
func (e *Env) ReloadTicket(t testing.TB, id int64) ticket.Ticket {
	t.Helper()
	var row ticket.Ticket
	e.load(t, &row, id)
	return row
}

// Follows returns the replies on a ticket in the order they were written.
func (e *Env) Follows(t testing.TB, ticketID int64) []ticket.Follow {
	t.Helper()
	var rows []ticket.Follow
	if err := e.DB.Where("ticket_id = ?", ticketID).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

// Announcement stores an announcement; its unset flags are stored false.
func (e *Env) Announcement(t testing.TB, row announcement.Announcement) *announcement.Announcement {
	t.Helper()
	for _, flag := range []**bool{&row.Show, &row.Pinned, &row.Popup} {
		if *flag == nil {
			*flag = new(bool)
		}
	}
	e.create(t, &row)
	return &row
}

// ReloadAnnouncement reads an announcement as stored.
func (e *Env) ReloadAnnouncement(t testing.TB, id int64) announcement.Announcement {
	t.Helper()
	var row announcement.Announcement
	e.load(t, &row, id)
	return row
}

// Ad stores an ad as given.
func (e *Env) Ad(t testing.TB, row ads.Ads) *ads.Ads {
	t.Helper()
	e.create(t, &row)
	return &row
}

// ReloadAd reads an ad as stored.
func (e *Env) ReloadAd(t testing.TB, id int64) ads.Ads {
	t.Helper()
	var row ads.Ads
	e.load(t, &row, id)
	return row
}

// Document stores a document; an unset switch shows it.
func (e *Env) Document(t testing.TB, row document.Document) *document.Document {
	t.Helper()
	if row.Show == nil {
		row.Show = new(true)
	}
	e.create(t, &row)
	return &row
}

// ReloadDocument reads a document as stored.
func (e *Env) ReloadDocument(t testing.TB, id int64) document.Document {
	t.Helper()
	var row document.Document
	e.load(t, &row, id)
	return row
}

// Count returns how many rows the table of model holds.
func (e *Env) Count(t testing.TB, model any) int64 {
	t.Helper()
	var n int64
	if err := e.DB.Model(model).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

// Task stores a task row as given.
func (e *Env) Task(t testing.TB, row task.Task) *task.Task {
	t.Helper()
	e.create(t, &row)
	return &row
}

// TaskError stores the failure of one target of a task.
func (e *Env) TaskError(t testing.TB, row task.TaskError) {
	t.Helper()
	e.create(t, &row)
}

// ReloadTask reads a task as stored.
func (e *Env) ReloadTask(t testing.TB, id int64) task.Task {
	t.Helper()
	var row task.Task
	e.load(t, &row, id)
	return row
}

// AllTasks returns every task row in the order they were created.
func (e *Env) AllTasks(t testing.TB) []task.Task {
	t.Helper()
	var rows []task.Task
	if err := e.DB.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}
