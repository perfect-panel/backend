package support

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/internal/repository"
)

// TicketStatistics is the part of the facade the platform's dashboard reads
// the ticket figures through, instead of the ticket repository (ADR-001
// rule 4).
type TicketStatistics interface {
	// CountTicketsAwaitingReply counts the tickets waiting for a staff reply.
	CountTicketsAwaitingReply(ctx context.Context) (int64, error)
}

// TicketReads is the part of the facade the other modules read tickets
// through instead of the ticket repository (ADR-001 rule 4): the dashboard
// figures and the Telegram bot's ticket views. The reads return the
// repository's results and errors as they are, so a miss is still
// gorm.ErrRecordNotFound.
type TicketReads interface {
	TicketStatistics
	// ListTickets pages the tickets of every user, only those in status when
	// it is set.
	ListTickets(ctx context.Context, page, size int, status *uint8) (int64, []*ticket.Ticket, error)
	// FindTicket returns the ticket; TicketDetails returns it with its
	// follow-ups.
	FindTicket(ctx context.Context, id int64) (*ticket.Ticket, error)
	TicketDetails(ctx context.Context, id int64) (*ticket.Details, error)
}

// ticketReads serves TicketReads from the ticket repository.
type ticketReads struct {
	tickets repository.TicketRepo
}

func (r ticketReads) CountTicketsAwaitingReply(ctx context.Context) (int64, error) {
	return r.tickets.QueryWaitReplyTotal(ctx)
}

func (r ticketReads) ListTickets(ctx context.Context, page, size int, status *uint8) (int64, []*ticket.Ticket, error) {
	return r.tickets.QueryTicketList(ctx, page, size, 0, status, "")
}

func (r ticketReads) FindTicket(ctx context.Context, id int64) (*ticket.Ticket, error) {
	return r.tickets.FindOne(ctx, id)
}

func (r ticketReads) TicketDetails(ctx context.Context, id int64) (*ticket.Details, error) {
	return r.tickets.QueryTicketDetail(ctx, id)
}
