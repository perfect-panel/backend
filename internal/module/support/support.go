// Package support is the facade of the support module: tickets,
// announcements, documents, ads and marketing tasks. Admin and public
// handlers call the same service; access-plane concerns such as auth and
// field trimming stay in the handlers. See docs/design/adr-001-modular-monolith.md.
package support

import (
	"context"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/module/support/internal/ads"
	"github.com/perfect-panel/backend/internal/module/support/internal/announcement"
	"github.com/perfect-panel/backend/internal/module/support/internal/document"
	"github.com/perfect-panel/backend/internal/module/support/internal/marketing"
	"github.com/perfect-panel/backend/internal/module/support/internal/repo"
	"github.com/perfect-panel/backend/internal/module/support/internal/ticket"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
)

// Service is the only surface other code may depend on; the implementation
// lives under internal/ where the compiler seals it off.
type Service interface {
	// TicketReads serves the dashboard's ticket figures and the bot's ticket
	// views.
	TicketReads

	CreateAnnouncement(ctx context.Context, req *dto.CreateAnnouncementRequest) error
	UpdateAnnouncement(ctx context.Context, req *dto.UpdateAnnouncementRequest) error
	DeleteAnnouncement(ctx context.Context, req *dto.DeleteAnnouncementRequest) error
	GetAnnouncement(ctx context.Context, req *dto.GetAnnouncementRequest) (*dto.Announcement, error)
	GetAnnouncementList(ctx context.Context, req *dto.GetAnnouncementListRequest) (*dto.GetAnnouncementListResponse, error)
	// QueryAnnouncement lists announcements visible to end users; Show=true is
	// enforced here, not trusted from the request.
	QueryAnnouncement(ctx context.Context, req *dto.QueryAnnouncementRequest) (*dto.QueryAnnouncementResponse, error)

	CreateAds(ctx context.Context, req *dto.CreateAdsRequest) error
	UpdateAds(ctx context.Context, req *dto.UpdateAdsRequest) error
	DeleteAds(ctx context.Context, req *dto.DeleteAdsRequest) error
	GetAdsDetail(ctx context.Context, req *dto.GetAdsDetailRequest) (*dto.Ads, error)
	GetAdsList(ctx context.Context, req *dto.GetAdsListRequest) (*dto.GetAdsListResponse, error)
	// GetPublicAds lists the active ads for the public site.
	GetPublicAds(ctx context.Context, req *dto.GetAdsRequest) (*dto.GetAdsResponse, error)

	CreateDocument(ctx context.Context, req *dto.CreateDocumentRequest) error
	UpdateDocument(ctx context.Context, req *dto.UpdateDocumentRequest) error
	DeleteDocument(ctx context.Context, req *dto.DeleteDocumentRequest) error
	BatchDeleteDocument(ctx context.Context, req *dto.BatchDeleteDocumentRequest) error
	GetDocumentDetail(ctx context.Context, req *dto.GetDocumentDetailRequest) (*dto.Document, error)
	GetDocumentList(ctx context.Context, req *dto.GetDocumentListRequest) (*dto.GetDocumentListResponse, error)
	// QueryDocumentDetail renders subscription-gated blocks for the current
	// user before returning the content.
	QueryDocumentDetail(ctx context.Context, req *dto.QueryDocumentDetailRequest) (*dto.Document, error)
	QueryDocumentList(ctx context.Context) (*dto.QueryDocumentListResponse, error)

	CreateTicketFollow(ctx context.Context, req *dto.CreateTicketFollowRequest) error
	GetTicketList(ctx context.Context, req *dto.GetTicketListRequest) (*dto.GetTicketListResponse, error)
	GetTicket(ctx context.Context, req *dto.GetTicketRequest) (*dto.Ticket, error)
	UpdateTicketStatus(ctx context.Context, req *dto.UpdateTicketStatusRequest) error
	// The user-facing ticket operations resolve the current user from the
	// request context and enforce ticket ownership.
	CreateUserTicket(ctx context.Context, req *dto.CreateUserTicketRequest) error
	CreateUserTicketFollow(ctx context.Context, req *dto.CreateUserTicketFollowRequest) error
	GetUserTicketDetails(ctx context.Context, req *dto.GetUserTicketDetailRequest) (*dto.Ticket, error)
	GetUserTicketList(ctx context.Context, req *dto.GetUserTicketListRequest) (*dto.GetUserTicketListResponse, error)
	UpdateUserTicketStatus(ctx context.Context, req *dto.UpdateUserTicketStatusRequest) error
	// UpdateTicketAsStaff applies a ticket change staff made outside the
	// admin panel (the Telegram bot): a reply, or a status change. It runs
	// the same use case as the admin panel's reply and status endpoints, so
	// the change is mirrored the same way, except back into the channel it
	// came from.
	UpdateTicketAsStaff(ctx context.Context, cmd *dto.StaffTicketUpdateCommand) (*dto.StaffTicketUpdateResult, error)

	CreateBatchSendEmailTask(ctx context.Context, req *dto.CreateBatchSendEmailTaskRequest) error
	GetPreSendEmailCount(ctx context.Context, req *dto.GetPreSendEmailCountRequest) (*dto.GetPreSendEmailCountResponse, error)
	GetBatchSendEmailTaskList(ctx context.Context, req *dto.GetBatchSendEmailTaskListRequest) (*dto.GetBatchSendEmailTaskListResponse, error)
	GetBatchSendEmailTaskStatus(ctx context.Context, req *dto.GetBatchSendEmailTaskStatusRequest) (*dto.GetBatchSendEmailTaskStatusResponse, error)
	StopBatchSendEmailTask(ctx context.Context, req *dto.StopBatchSendEmailTaskRequest) error
	CreateQuotaTask(ctx context.Context, req *dto.CreateQuotaTaskRequest) error
	QueryQuotaTaskList(ctx context.Context, req *dto.QueryQuotaTaskListRequest) (*dto.QueryQuotaTaskListResponse, error)
	QueryQuotaTaskPreCount(ctx context.Context, req *dto.QueryQuotaTaskPreCountRequest) (*dto.QueryQuotaTaskPreCountResponse, error)
}

// SubscriptionReader is the support module's port onto the subscription
// domain (dependency inversion: the consumer owns the interface); the
// composition root adapts the subscription domain's repository to it.
type SubscriptionReader interface {
	HasActiveSubscription(ctx context.Context, userID int64) (bool, error)
}

// EmailRecipientReader is the module's port onto the identity domain for
// selecting campaign recipients; the identity user repository satisfies it
// structurally.
type EmailRecipientReader interface {
	QueryEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) ([]string, error)
	CountEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) (int64, error)
}

// SubscriptionSelector is the module's port onto the subscription domain for
// selecting quota-task targets; the subscription domain's user-subscription
// repository satisfies it structurally.
type SubscriptionSelector interface {
	QuerySubscribeIdsByFilter(ctx context.Context, filter *usersub.SubscribeFilter) ([]int64, error)
	CountSubscribesByFilter(ctx context.Context, filter *usersub.SubscribeFilter) (int64, error)
}

// MarketingQueue schedules asynchronous execution of marketing tasks; the
// composition root adapts the asynq client so queue task types stay out of
// the module.
type MarketingQueue interface {
	EnqueueBatchEmail(ctx context.Context, taskID int64, processAt time.Time) (queueTaskID string, err error)
	EnqueueQuota(ctx context.Context, taskID int64) error
}

// BatchEmailStopper aborts a running batch-email worker, if any.
type BatchEmailStopper interface {
	StopBatchEmail(taskID int64)
}

// AuditLog records the administrators' mutations (ticket replies and status
// changes, marketing tasks) in the platform's system log; the platform
// kernel's log repository satisfies it.
type AuditLog interface {
	Insert(ctx context.Context, data *log.SystemLog) error
}

// Deps declares everything the module needs; the composition root
// (internal/app) provides them. The ticket, announcement, ads and document
// repositories are the module's own (see NewRepoBuilder); the rest are ports
// onto other domains.
type Deps struct {
	Announcements repository.AnnouncementRepo
	Ads           repository.AdsRepo
	Documents     repository.DocumentRepo
	Tickets       repository.TicketRepo
	Tasks         repository.TaskRepo
	Subscriptions SubscriptionReader
	Recipients    EmailRecipientReader
	QuotaTargets  SubscriptionSelector
	Queue         MarketingQueue
	EmailStopper  BatchEmailStopper
	// TicketNotify mirrors ticket lifecycle into the Telegram admin group;
	// nil disables the mirror. Best-effort by contract.
	TicketNotify ticket.Notifier
	// Redis backs the per-user ticket creation and reply limits; nil
	// disables them.
	Redis *redis.Client
	// AuditLogs records the administrators' mutations; nil records nothing.
	AuditLogs AuditLog
}

// NewRepoBuilder exports the module-owned repository implementations for
// store assembly (ADR-001 step-6 preparation).
func NewRepoBuilder() repository.SupportBuilder {
	return func(c repository.ModuleConn) repository.SupportRepos {
		conn := c.Conn()
		return repository.SupportRepos{
			Tickets:       repo.NewTicketRepo(conn),
			Announcements: repo.NewAnnouncementRepo(conn),
			Ads:           repo.NewAdsRepo(conn),
			Documents:     repo.NewDocumentRepo(conn),
		}
	}
}

func New(deps Deps) Service {
	return &service{
		ticketReads: ticketReads{tickets: deps.Tickets},

		announcements: announcement.NewService(deps.Announcements),
		ads:           ads.NewService(deps.Ads),
		documents:     document.NewService(deps.Documents, deps.Subscriptions),
		tickets: ticket.NewService(deps.Tickets, deps.TicketNotify, ticket.Limits{
			Creation: ticket.NewCreationLimiter(deps.Redis),
			Follows:  ticket.NewFollowLimiter(deps.Redis),
		}, deps.AuditLogs),
		marketing: marketing.NewService(deps.Tasks, deps.Recipients, deps.QuotaTargets, deps.Queue, deps.EmailStopper, deps.AuditLogs),
	}
}

type service struct {
	ticketReads
	announcements *announcement.Service
	ads           *ads.Service
	documents     *document.Service
	tickets       *ticket.Service
	marketing     *marketing.Service
}

func (s *service) CreateAnnouncement(ctx context.Context, req *dto.CreateAnnouncementRequest) error {
	return s.announcements.Create(ctx, req)
}

func (s *service) UpdateAnnouncement(ctx context.Context, req *dto.UpdateAnnouncementRequest) error {
	return s.announcements.Update(ctx, req)
}

func (s *service) DeleteAnnouncement(ctx context.Context, req *dto.DeleteAnnouncementRequest) error {
	return s.announcements.Delete(ctx, req)
}

func (s *service) GetAnnouncement(ctx context.Context, req *dto.GetAnnouncementRequest) (*dto.Announcement, error) {
	return s.announcements.Get(ctx, req)
}

func (s *service) GetAnnouncementList(ctx context.Context, req *dto.GetAnnouncementListRequest) (*dto.GetAnnouncementListResponse, error) {
	return s.announcements.List(ctx, req)
}

func (s *service) QueryAnnouncement(ctx context.Context, req *dto.QueryAnnouncementRequest) (*dto.QueryAnnouncementResponse, error) {
	return s.announcements.QueryVisible(ctx, req)
}

func (s *service) CreateAds(ctx context.Context, req *dto.CreateAdsRequest) error {
	return s.ads.Create(ctx, req)
}

func (s *service) UpdateAds(ctx context.Context, req *dto.UpdateAdsRequest) error {
	return s.ads.Update(ctx, req)
}

func (s *service) DeleteAds(ctx context.Context, req *dto.DeleteAdsRequest) error {
	return s.ads.Delete(ctx, req)
}

func (s *service) GetAdsDetail(ctx context.Context, req *dto.GetAdsDetailRequest) (*dto.Ads, error) {
	return s.ads.GetDetail(ctx, req)
}

func (s *service) GetAdsList(ctx context.Context, req *dto.GetAdsListRequest) (*dto.GetAdsListResponse, error) {
	return s.ads.List(ctx, req)
}

func (s *service) CreateDocument(ctx context.Context, req *dto.CreateDocumentRequest) error {
	return s.documents.Create(ctx, req)
}

func (s *service) UpdateDocument(ctx context.Context, req *dto.UpdateDocumentRequest) error {
	return s.documents.Update(ctx, req)
}

func (s *service) DeleteDocument(ctx context.Context, req *dto.DeleteDocumentRequest) error {
	return s.documents.Delete(ctx, req)
}

func (s *service) BatchDeleteDocument(ctx context.Context, req *dto.BatchDeleteDocumentRequest) error {
	return s.documents.BatchDelete(ctx, req)
}

func (s *service) GetDocumentDetail(ctx context.Context, req *dto.GetDocumentDetailRequest) (*dto.Document, error) {
	return s.documents.GetDetail(ctx, req)
}

func (s *service) GetDocumentList(ctx context.Context, req *dto.GetDocumentListRequest) (*dto.GetDocumentListResponse, error) {
	return s.documents.List(ctx, req)
}

func (s *service) QueryDocumentDetail(ctx context.Context, req *dto.QueryDocumentDetailRequest) (*dto.Document, error) {
	return s.documents.QueryDetail(ctx, req)
}

func (s *service) QueryDocumentList(ctx context.Context) (*dto.QueryDocumentListResponse, error) {
	return s.documents.QueryList(ctx)
}

func (s *service) CreateTicketFollow(ctx context.Context, req *dto.CreateTicketFollowRequest) error {
	return s.tickets.CreateFollow(ctx, req)
}

func (s *service) GetTicketList(ctx context.Context, req *dto.GetTicketListRequest) (*dto.GetTicketListResponse, error) {
	return s.tickets.List(ctx, req)
}

func (s *service) GetTicket(ctx context.Context, req *dto.GetTicketRequest) (*dto.Ticket, error) {
	return s.tickets.GetDetail(ctx, req)
}

func (s *service) UpdateTicketStatus(ctx context.Context, req *dto.UpdateTicketStatusRequest) error {
	return s.tickets.UpdateStatus(ctx, req)
}

func (s *service) CreateUserTicket(ctx context.Context, req *dto.CreateUserTicketRequest) error {
	return s.tickets.CreateUserTicket(ctx, req)
}

func (s *service) CreateUserTicketFollow(ctx context.Context, req *dto.CreateUserTicketFollowRequest) error {
	return s.tickets.CreateUserFollow(ctx, req)
}

func (s *service) GetUserTicketDetails(ctx context.Context, req *dto.GetUserTicketDetailRequest) (*dto.Ticket, error) {
	return s.tickets.GetUserDetail(ctx, req)
}

func (s *service) GetUserTicketList(ctx context.Context, req *dto.GetUserTicketListRequest) (*dto.GetUserTicketListResponse, error) {
	return s.tickets.GetUserList(ctx, req)
}

func (s *service) UpdateUserTicketStatus(ctx context.Context, req *dto.UpdateUserTicketStatusRequest) error {
	return s.tickets.UpdateUserStatus(ctx, req)
}

func (s *service) UpdateTicketAsStaff(ctx context.Context, cmd *dto.StaffTicketUpdateCommand) (*dto.StaffTicketUpdateResult, error) {
	return s.tickets.UpdateAsStaff(ctx, cmd)
}

func (s *service) CreateBatchSendEmailTask(ctx context.Context, req *dto.CreateBatchSendEmailTaskRequest) error {
	return s.marketing.CreateBatchSendEmailTask(ctx, req)
}

func (s *service) GetPreSendEmailCount(ctx context.Context, req *dto.GetPreSendEmailCountRequest) (*dto.GetPreSendEmailCountResponse, error) {
	return s.marketing.GetPreSendEmailCount(ctx, req)
}

func (s *service) GetBatchSendEmailTaskList(ctx context.Context, req *dto.GetBatchSendEmailTaskListRequest) (*dto.GetBatchSendEmailTaskListResponse, error) {
	return s.marketing.GetBatchSendEmailTaskList(ctx, req)
}

func (s *service) GetBatchSendEmailTaskStatus(ctx context.Context, req *dto.GetBatchSendEmailTaskStatusRequest) (*dto.GetBatchSendEmailTaskStatusResponse, error) {
	return s.marketing.GetBatchSendEmailTaskStatus(ctx, req)
}

func (s *service) StopBatchSendEmailTask(ctx context.Context, req *dto.StopBatchSendEmailTaskRequest) error {
	return s.marketing.StopBatchSendEmailTask(ctx, req)
}

func (s *service) CreateQuotaTask(ctx context.Context, req *dto.CreateQuotaTaskRequest) error {
	return s.marketing.CreateQuotaTask(ctx, req)
}

func (s *service) QueryQuotaTaskList(ctx context.Context, req *dto.QueryQuotaTaskListRequest) (*dto.QueryQuotaTaskListResponse, error) {
	return s.marketing.QueryQuotaTaskList(ctx, req)
}

func (s *service) QueryQuotaTaskPreCount(ctx context.Context, req *dto.QueryQuotaTaskPreCountRequest) (*dto.QueryQuotaTaskPreCountResponse, error) {
	return s.marketing.QueryQuotaTaskPreCount(ctx, req)
}

func (s *service) GetPublicAds(ctx context.Context, req *dto.GetAdsRequest) (*dto.GetAdsResponse, error) {
	return s.ads.GetPublicAds(ctx, req)
}
