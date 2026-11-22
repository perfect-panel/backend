package supporttest

import (
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/module/support/entity/ads"
	"github.com/perfect-panel/backend/internal/module/support/entity/announcement"
	"github.com/perfect-panel/backend/internal/module/support/entity/document"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
)

// The views below are how the API shows a stored row, its times in Unix
// milliseconds: what a test expects a handler or service to answer for the
// rows it reloaded.

// AdView shows a stored ad.
func AdView(ad ads.Ads) dto.Ads {
	return dto.Ads{Id: int(ad.Id), Title: ad.Title, Type: ad.Type, Content: ad.Content, Description: ad.Description, TargetURL: ad.TargetURL,
		StartTime: ad.StartTime.UnixMilli(), EndTime: ad.EndTime.UnixMilli(), Status: ad.Status,
		CreatedAt: ad.CreatedAt.UnixMilli(), UpdatedAt: ad.UpdatedAt.UnixMilli()}
}

// AnnouncementView shows a stored announcement.
func AnnouncementView(a announcement.Announcement) dto.Announcement {
	return dto.Announcement{Id: a.Id, Title: a.Title, Content: a.Content, Show: a.Show, Pinned: a.Pinned, Popup: a.Popup,
		CreatedAt: a.CreatedAt.UnixMilli(), UpdatedAt: a.UpdatedAt.UnixMilli()}
}

// DocumentView shows a stored document with its content as written and
// the tags given, which a test states rather than splitting the stored
// column the way the code under test does.
func DocumentView(d document.Document, tags ...string) dto.Document {
	if tags == nil {
		tags = []string{}
	}
	return dto.Document{Id: d.Id, Title: d.Title, Content: d.Content, Tags: tags, Show: d.Show != nil && *d.Show,
		CreatedAt: d.CreatedAt.UnixMilli(), UpdatedAt: d.UpdatedAt.UnixMilli()}
}

// TicketView shows a stored ticket with the follows given, in their order.
func TicketView(t ticket.Ticket, follows ...ticket.Follow) dto.Ticket {
	view := dto.Ticket{Id: t.Id, Title: t.Title, Description: t.Description, UserId: t.UserId, Status: t.Status,
		CreatedAt: t.CreatedAt.UnixMilli(), UpdatedAt: t.UpdatedAt.UnixMilli()}
	for _, f := range follows {
		view.Follows = append(view.Follows, dto.Follow{Id: f.Id, TicketId: f.TicketId, From: f.From, Type: f.Type, Content: f.Content,
			CreatedAt: f.CreatedAt.UnixMilli()})
	}
	return view
}
