// Package ticket implements the ticket subdomain of the support module. Only
// the module facade (internal/module/support) may reach it.
package ticket

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/perfect-panel/backend/internal/infra/mapping"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	entity "github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Notifier mirrors ticket lifecycle into other channels — today the forum
// topic in the Telegram admin group. Implementations are best-effort by
// contract: they never fail the ticket operation, so the methods return
// nothing and do their own logging.
type Notifier interface {
	TicketCreated(ctx context.Context, t *entity.Ticket)
	TicketReplied(ctx context.Context, ticketID int64, from, content string)
	TicketStatusChanged(ctx context.Context, ticketID int64, status uint8)
}

// AuditLog records the administrators' ticket mutations in the platform's
// system log; the platform kernel's log repository satisfies it.
type AuditLog interface {
	Insert(ctx context.Context, data *log.SystemLog) error
}

// Limits are the per-user caps of the user-facing ticket writes; a nil
// limiter turns its cap off.
type Limits struct {
	// Creation caps how many tickets one user opens per window.
	Creation CreationLimiter
	// Follows caps how many replies one user writes per window: every reply
	// is mirrored into the Telegram admin group.
	Follows CreationLimiter
}

// Service runs the ticket desk for the support facade.
type Service struct {
	repo   repository.TicketRepo
	notify Notifier
	limits Limits
	audit  AuditLog
}

// NewService builds the ticket service; notify may be nil when no mirror
// channel is wired, the limits nil when the user writes are not rate
// limited, audit nil when the administrators' actions are not recorded.
func NewService(repo repository.TicketRepo, notify Notifier, limits Limits, audit AuditLog) *Service {
	return &Service{repo: repo, notify: notify, limits: limits, audit: audit}
}

func currentUser(ctx context.Context) (*user.User, error) {
	u, ok := user.FromContext(ctx)
	if !ok {
		logger.WithContext(ctx).Error("current user is not found in context")
		return nil, xerr.Wrapf(errors.New("no authenticated user"), xerr.InvalidAccess, "Invalid Access")
	}
	return u, nil
}

// allow takes one permit of limiter for the user; what is the write being
// limited, for the refusal. The limit fails open: it protects staff from
// floods, not the ticket desk from a Redis outage.
func allow(ctx context.Context, limiter CreationLimiter, userID int64, what string) error {
	if limiter == nil {
		return nil
	}
	allowed, err := limiter.Allow(ctx, userID)
	switch {
	case err != nil:
		logger.WithContext(ctx).Errorw("[Ticket] rate limit check failed", logger.Field("error", err.Error()), logger.Field("user_id", userID), logger.Field("limit", what))
	case !allowed:
		return xerr.Wrapf(errors.New("rate limited"), xerr.TooManyRequests, "%s limit exceeded for user %d", what, userID)
	}
	return nil
}

// change is one write to a ticket: the follow to append, if any, and the
// status the ticket moves to.
type change struct {
	ticketID int64
	// ownerID scopes the status update to the ticket's owner; zero for staff.
	ownerID int64
	// follow is nil when only the status changes.
	follow *entity.Follow
	status uint8
	// mirror reports whether the notifier mirrors the change. It is false
	// when the change was made inside the mirror channel itself.
	mirror bool
}

// apply is the one "append follow / update status" use case behind every
// ticket reply and status change, whichever channel it comes from: the
// admin panel, the user site or the Telegram bot. A reply is mirrored as a
// reply and a status change as a status change.
func (s *Service) apply(ctx context.Context, c change) error {
	if c.follow != nil {
		if err := s.repo.InsertTicketFollow(ctx, c.follow); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseInsertError, "create ticket follow failed: %v", err)
		}
	}
	if err := s.repo.UpdateTicketStatus(ctx, c.ticketID, c.ownerID, c.status); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update ticket status failed: %v", err)
	}
	if !c.mirror || s.notify == nil {
		return nil
	}
	if c.follow != nil {
		s.notify.TicketReplied(ctx, c.ticketID, c.follow.From, c.follow.Content)
	} else {
		s.notify.TicketStatusChanged(ctx, c.ticketID, c.status)
	}
	return nil
}

// findTicket loads the ticket a change applies to.
func (s *Service) findTicket(ctx context.Context, id int64) (*entity.Ticket, error) {
	t, err := s.repo.FindOne(ctx, id)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find ticket %d failed: %v", id, err)
	}
	return t, nil
}

// staffMayReply refuses a staff reply to a closed ticket: closing ends the
// conversation for staff, who reopen the ticket explicitly (a status change
// to Pending) before writing again. The refusal carries entity.ErrClosed for
// the channels to name it.
func staffMayReply(t *entity.Ticket) error {
	if t.Status != entity.Closed {
		return nil
	}
	return fmt.Errorf("%w: %w", entity.ErrClosed, xerr.NewErrCodeMsg(xerr.InvalidParams, "the ticket is closed; reopen it before replying"))
}

// recordAdminAction writes the administrator's ticket mutation to the audit
// trail. The mutation is already stored; a trail that cannot be written is
// logged, not reported as the mutation's failure.
func (s *Service) recordAdminAction(ctx context.Context, action log.AdminAction) {
	if s.audit == nil {
		return
	}
	action.Object = "ticket"
	row, err := log.NewAdminActionLog(log.AdminActionFrom(ctx, action))
	if err == nil {
		err = s.audit.Insert(ctx, row)
	}
	if err != nil {
		logger.WithContext(ctx).Errorw("[Ticket] record admin action failed", logger.Field("error", err.Error()),
			logger.Field("action", action.Action), logger.Field("ticket_id", action.ObjectID))
	}
}

// CreateFollow appends an admin reply and flips the ticket back to Waiting.
// A closed ticket takes no reply until it is reopened.
func (s *Service) CreateFollow(ctx context.Context, req *dto.CreateTicketFollowRequest) error {
	t, err := s.findTicket(ctx, req.TicketId)
	if err != nil {
		return err
	}
	if err := staffMayReply(t); err != nil {
		return err
	}
	if err := s.apply(ctx, change{
		ticketID: req.TicketId,
		follow: &entity.Follow{
			TicketId: req.TicketId,
			From:     req.From,
			Type:     req.Type,
			Content:  req.Content,
		},
		status: entity.Waiting,
		mirror: true,
	}); err != nil {
		return err
	}
	s.recordAdminAction(ctx, log.AdminAction{Action: "ticket.reply", ObjectID: req.TicketId})
	return nil
}

// UpdateAsStaff applies a ticket change staff made outside the admin panel:
// a reply is appended as a text follow and moves the ticket to Waiting, like
// a reply from the admin panel; otherwise the ticket moves to cmd.Status. A
// reply to a closed ticket is refused with entity.ErrClosed; a status change
// is the explicit reopen. A missing ticket is an error whose chain holds
// gorm.ErrRecordNotFound. The channel records its own audit trail.
func (s *Service) UpdateAsStaff(ctx context.Context, cmd *dto.StaffTicketUpdateCommand) (*dto.StaffTicketUpdateResult, error) {
	c := change{ticketID: cmd.TicketId, status: cmd.Status, mirror: !cmd.FromMirror}
	if cmd.Reply != "" {
		c.status = entity.Waiting
		c.follow = &entity.Follow{
			TicketId: cmd.TicketId,
			From:     cmd.From,
			Type:     entity.FollowText,
			Content:  cmd.Reply,
		}
	} else if !validStatus(cmd.Status) {
		return nil, xerr.Wrapf(errors.New("unknown ticket status"), xerr.InvalidParams, "ticket status %d is not a ticket status", cmd.Status)
	}
	t, err := s.findTicket(ctx, cmd.TicketId)
	if err != nil {
		return nil, err
	}
	if c.follow != nil {
		if err := staffMayReply(t); err != nil {
			return nil, err
		}
	}
	if err := s.apply(ctx, c); err != nil {
		return nil, err
	}
	return &dto.StaffTicketUpdateResult{PreviousStatus: t.Status}, nil
}

func validStatus(status uint8) bool {
	switch status {
	case entity.Pending, entity.Waiting, entity.Processed, entity.Closed:
		return true
	}
	return false
}

// List pages the tickets of every user, or of one, for the admin panel.
func (s *Service) List(ctx context.Context, req *dto.GetTicketListRequest) (*dto.GetTicketListResponse, error) {
	total, list, err := s.repo.QueryTicketList(ctx, int(req.Page), int(req.Size), req.UserId, req.Status, req.Search)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "QueryTicketList error: %v", err)
	}
	resp := &dto.GetTicketListResponse{
		Total: total,
		List:  make([]dto.Ticket, 0),
	}
	if err := mapping.Copy(&resp.List, list); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "copy the ticket list")
	}
	return resp, nil
}

// GetDetail returns a ticket with its follows for the admin panel.
func (s *Service) GetDetail(ctx context.Context, req *dto.GetTicketRequest) (*dto.Ticket, error) {
	data, err := s.repo.QueryTicketDetail(ctx, req.Id)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get ticket detail failed: %v", err)
	}
	resp := &dto.Ticket{}
	if err := mapping.Copy(resp, data); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "copy ticket %d", req.Id)
	}
	return resp, nil
}

// UpdateStatus moves a ticket to the status staff chose in the admin panel.
// As for a change from the bot, the status must be one of the four ticket
// statuses and the ticket must exist; otherwise nothing is stored or
// mirrored.
func (s *Service) UpdateStatus(ctx context.Context, req *dto.UpdateTicketStatusRequest) error {
	if !validStatus(*req.Status) {
		return xerr.Wrapf(errors.New("unknown ticket status"), xerr.InvalidParams, "ticket status %d is not a ticket status", *req.Status)
	}
	if _, err := s.findTicket(ctx, req.Id); err != nil {
		return err
	}
	if err := s.apply(ctx, change{ticketID: req.Id, status: *req.Status, mirror: true}); err != nil {
		return err
	}
	s.recordAdminAction(ctx, log.AdminAction{Action: "ticket.status", ObjectID: req.Id, Detail: fmt.Sprintf("status=%d", *req.Status)})
	return nil
}

// CreateUserTicket opens a ticket for the current user, within the creation
// limit, and mirrors it.
func (s *Service) CreateUserTicket(ctx context.Context, req *dto.CreateUserTicketRequest) error {
	u, err := currentUser(ctx)
	if err != nil {
		return err
	}
	if err := allow(ctx, s.limits.Creation, u.Id, "ticket creation"); err != nil {
		return err
	}
	// Insert backfills the id, which the mirror channel needs for its topic.
	t := &entity.Ticket{
		Title:       req.Title,
		Description: req.Description,
		UserId:      u.Id,
		Status:      entity.Pending,
	}
	if err := s.repo.Insert(ctx, t); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "insert ticket error: %v", err)
	}
	if s.notify != nil {
		s.notify.TicketCreated(ctx, t)
	}
	return nil
}

// ownTicket loads a ticket the current user acts on and refuses anyone but
// its owner.
func (s *Service) ownTicket(ctx context.Context, u *user.User, id int64) error {
	t, err := s.findTicket(ctx, id)
	if err != nil {
		return err
	}
	if t.UserId != u.Id {
		logger.WithContext(ctx).Errorw("[Ticket] Invalid access", logger.Field("user_id", u.Id), logger.Field("ticket_user_id", t.UserId))
		return xerr.Wrapf(errors.New("not the ticket owner"), xerr.InvalidAccess, "invalid access")
	}
	return nil
}

// CreateUserFollow appends a user reply, within the reply limit, after
// verifying ticket ownership, and flips the ticket to Pending. This is the
// product rule for closed tickets as well: the owner's reply reopens a
// closed ticket, since the owner has no other way to say the matter is not
// settled; staff, in contrast, must reopen a ticket explicitly before
// replying. The author is always the ticket owner: a client-supplied "from"
// would let a user post what renders as a staff reply.
func (s *Service) CreateUserFollow(ctx context.Context, req *dto.CreateUserTicketFollowRequest) error {
	u, err := currentUser(ctx)
	if err != nil {
		return err
	}
	followType, err := userFollowType(req.Type, req.Content)
	if err != nil {
		return err
	}
	if err := allow(ctx, s.limits.Follows, u.Id, "ticket reply"); err != nil {
		return err
	}
	if err := s.ownTicket(ctx, u, req.TicketId); err != nil {
		return err
	}
	return s.apply(ctx, change{
		ticketID: req.TicketId,
		ownerID:  u.Id,
		follow: &entity.Follow{
			TicketId: req.TicketId,
			From:     entity.FromUser,
			Type:     followType,
			Content:  req.Content,
		},
		status: entity.Pending,
		mirror: true,
	})
}

// rasterDataURLPrefixes are the inline image encodings a user may attach: the
// web client re-encodes uploads to WebP and sends them as base64 data URLs.
// SVG is excluded because it is a document that can carry script.
var rasterDataURLPrefixes = []string{
	"data:image/webp;base64,",
	"data:image/png;base64,",
	"data:image/jpeg;base64,",
	"data:image/gif;base64,",
}

// userFollowType validates what a ticket owner may post: text, or an image
// the clients render as <img src> — an inline raster data URL or an absolute
// http(s) URL. An omitted type is text, as the column default stores it.
func userFollowType(followType uint8, content string) (uint8, error) {
	switch followType {
	case 0, entity.FollowText:
		return entity.FollowText, nil
	case entity.FollowImage:
		if !isImageReference(content) {
			return 0, xerr.Wrapf(errors.New("unsupported image reference"), xerr.InvalidParams, "image follow must be an inline raster image or an http(s) URL")
		}
		return entity.FollowImage, nil
	}
	return 0, xerr.Wrapf(errors.New("unsupported follow type"), xerr.InvalidParams, "unsupported follow type %d", followType)
}

func isImageReference(content string) bool {
	for _, prefix := range rasterDataURLPrefixes {
		if len(content) > len(prefix) && strings.EqualFold(content[:len(prefix)], prefix) {
			return true
		}
	}
	u, err := url.Parse(content)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// GetUserDetail returns one of the current user's tickets with its follows.
func (s *Service) GetUserDetail(ctx context.Context, req *dto.GetUserTicketDetailRequest) (*dto.Ticket, error) {
	data, err := s.repo.QueryTicketDetail(ctx, req.Id)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get ticket detail failed: %v", err)
	}
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if data.UserId != u.Id {
		return nil, xerr.Wrapf(errors.New("not the ticket owner"), xerr.InvalidAccess, "invalid access")
	}
	resp := &dto.Ticket{}
	if err := mapping.Copy(resp, data); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "copy ticket %d", req.Id)
	}
	return resp, nil
}

// GetUserList pages the current user's tickets.
func (s *Service) GetUserList(ctx context.Context, req *dto.GetUserTicketListRequest) (*dto.GetUserTicketListResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	total, list, err := s.repo.QueryTicketList(ctx, req.Page, req.Size, u.Id, req.Status, req.Search)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "QueryTicketList error: %v", err)
	}
	resp := &dto.GetUserTicketListResponse{
		Total: total,
		List:  make([]dto.Ticket, 0),
	}
	if err := mapping.Copy(&resp.List, list); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "copy the ticket list")
	}
	return resp, nil
}

// UpdateUserStatus lets a user close their own ticket. Closing is the only
// status change the product offers users: Waiting and Processed are staff
// decisions, and a reply is what moves a ticket back to Pending. Ownership is
// checked before anything changes, because the status mirror reaches the
// Telegram admin group.
func (s *Service) UpdateUserStatus(ctx context.Context, req *dto.UpdateUserTicketStatusRequest) error {
	u, err := currentUser(ctx)
	if err != nil {
		return err
	}
	if req.Status == nil || *req.Status != entity.Closed {
		return xerr.Wrapf(errors.New("unsupported status change"), xerr.InvalidParams, "users may only close tickets")
	}
	if err := s.ownTicket(ctx, u, req.Id); err != nil {
		return err
	}
	return s.apply(ctx, change{ticketID: req.Id, ownerID: u.Id, status: entity.Closed, mirror: true})
}
