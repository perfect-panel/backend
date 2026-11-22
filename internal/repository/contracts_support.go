package repository

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/support/entity/ads"
	"github.com/perfect-panel/backend/internal/module/support/entity/announcement"
	"github.com/perfect-panel/backend/internal/module/support/entity/document"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
)

// TicketRepo manages the support tickets and their follow-up messages.
type TicketRepo interface {
	Insert(ctx context.Context, data *ticket.Ticket) error
	FindOne(ctx context.Context, id int64) (*ticket.Ticket, error)
	Update(ctx context.Context, data *ticket.Ticket) error
	Delete(ctx context.Context, id int64) error
	QueryTicketDetail(ctx context.Context, id int64) (*ticket.Details, error)
	InsertTicketFollow(ctx context.Context, data *ticket.Follow) error
	QueryTicketList(ctx context.Context, page, size int, userId int64, status *uint8, search string) (int64, []*ticket.Ticket, error)
	UpdateTicketStatus(ctx context.Context, id, userId int64, status uint8) error
	QueryWaitReplyTotal(ctx context.Context) (int64, error)
}

// AnnouncementRepo manages the announcements shown to users.
type AnnouncementRepo interface {
	Insert(ctx context.Context, data *announcement.Announcement) error
	FindOne(ctx context.Context, id int64) (*announcement.Announcement, error)
	Update(ctx context.Context, data *announcement.Announcement) error
	Delete(ctx context.Context, id int64) error
	GetAnnouncementListByPage(ctx context.Context, page, size int, filter announcement.Filter) (int64, []*announcement.Announcement, error)
}

// AdsRepo manages the advertisements the clients display.
type AdsRepo interface {
	Insert(ctx context.Context, data *ads.Ads) error
	FindOne(ctx context.Context, id int64) (*ads.Ads, error)
	Update(ctx context.Context, data *ads.Ads) error
	Delete(ctx context.Context, id int64) error
	GetAdsListByPage(ctx context.Context, page, size int, filter ads.Filter) (int64, []*ads.Ads, error)
}

// DocumentRepo manages the help documents users browse.
type DocumentRepo interface {
	Insert(ctx context.Context, data *document.Document) error
	FindOne(ctx context.Context, id int64) (*document.Document, error)
	Update(ctx context.Context, data *document.Document) error
	Delete(ctx context.Context, id int64) error
	QueryDocumentDetail(ctx context.Context, id int64) (*document.Document, error)
	QueryDocumentList(ctx context.Context, page, size int, tag string, search string) (int64, []*document.Document, error)
	GetDocumentListByAll(ctx context.Context) (int64, []*document.Document, error)
}
