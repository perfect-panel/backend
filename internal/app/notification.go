package app

import (
	"context"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	walletEntity "github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/notification"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription"
	subscriptiondto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/support"
	supportdto "github.com/perfect-panel/backend/internal/module/support/contract"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/internal/repository"
)

// newNotificationModule wires the notification module; the bot client is
// runtime-recreated, so the module reads it per call. The bot's ports onto
// the other domains are backed by their facades (ADR-001 rules 2 and 4), so
// ticket changes are mirrored like any other and subscription changes run the
// admin panel's use cases.
func newNotificationModule(store repository.Store, srv *Application) notification.Service {
	return notification.New(notification.Deps{
		Bot:         srv.Runtime.TelegramBot,
		GroupChatID: func() int64 { return srv.Runtime.Config().Telegram.GroupChatID },
		Topics:      store.TelegramTopic(),
		Redis:       srv.Redis,
		Accounts: botAccounts{
			identity: srv.Identity,
		},
		Tickets:       botTickets{support: srv.Support},
		Subscriptions: botSubscriptions{subscription: srv.Subscription},
		Billing:       botBilling{billing: srv.Billing},
		AuditLogs:     botAuditLogs{logs: store.Log()},
	})
}

// botAccounts backs the bot's identity port with the identity facade. The
// Telegram binding and the ban run identity's own use cases: a ban drops the
// account's subscription-token caches and node user lists like the admin
// panel's disable does.
type botAccounts struct {
	identity botIdentity
}

// botIdentity is the part of the identity facade the bot uses.
type botIdentity interface {
	FindUser(ctx context.Context, id int64) (*user.User, error)
	FindAuthMethodByIdentifier(ctx context.Context, authType, identifier string) (*user.AuthMethods, error)
	FindUserAuthMethod(ctx context.Context, userID int64, authType string) (*user.AuthMethods, error)
	ListUserAuthMethods(ctx context.Context, userID int64) ([]*user.AuthMethods, error)
	BindTelegramChat(ctx context.Context, userID int64, chatID string) error
	SetUserEnabled(ctx context.Context, userID int64, enabled bool) error
	CountRegisteredUsersOn(ctx context.Context, day time.Time) (int64, error)
}

var _ notification.Accounts = botAccounts{}

func (a botAccounts) FindUser(ctx context.Context, id int64) (*user.User, error) {
	return a.identity.FindUser(ctx, id)
}

func (a botAccounts) FindBinding(ctx context.Context, authType, identifier string) (*user.AuthMethods, error) {
	return a.identity.FindAuthMethodByIdentifier(ctx, authType, identifier)
}

func (a botAccounts) FindUserBinding(ctx context.Context, userID int64, authType string) (*user.AuthMethods, error) {
	return a.identity.FindUserAuthMethod(ctx, userID, authType)
}

func (a botAccounts) ListBindings(ctx context.Context, userID int64) ([]*user.AuthMethods, error) {
	return a.identity.ListUserAuthMethods(ctx, userID)
}

func (a botAccounts) BindTelegram(ctx context.Context, userID int64, chatID string) error {
	return a.identity.BindTelegramChat(ctx, userID, chatID)
}

func (a botAccounts) SetEnabled(ctx context.Context, userID int64, enabled bool) error {
	return a.identity.SetUserEnabled(ctx, userID, enabled)
}

func (a botAccounts) CountRegistrations(ctx context.Context, t time.Time) (int64, error) {
	return a.identity.CountRegisteredUsersOn(ctx, t)
}

// botTickets backs the bot's ticket port with the support facade: its
// ticket reads, and the staff update, whose notifier mirrors the change into
// the ticket's topic.
type botTickets struct {
	support support.Service
}

func (t botTickets) CountAwaitingReply(ctx context.Context) (int64, error) {
	return t.support.CountTicketsAwaitingReply(ctx)
}

func (t botTickets) List(ctx context.Context, page, size int, status *uint8) (int64, []*ticket.Ticket, error) {
	return t.support.ListTickets(ctx, page, size, status)
}

func (t botTickets) Find(ctx context.Context, id int64) (*ticket.Ticket, error) {
	return t.support.FindTicket(ctx, id)
}

func (t botTickets) Detail(ctx context.Context, id int64) (*ticket.Details, error) {
	return t.support.TicketDetails(ctx, id)
}

func (t botTickets) Reply(ctx context.Context, id int64, from, content string, inTopic bool) (uint8, error) {
	result, err := t.support.UpdateTicketAsStaff(ctx, &supportdto.StaffTicketUpdateCommand{
		TicketId:   id,
		Reply:      content,
		From:       from,
		FromMirror: inTopic,
	})
	if err != nil {
		return 0, err
	}
	return result.PreviousStatus, nil
}

func (t botTickets) SetStatus(ctx context.Context, id int64, status uint8, inTopic bool) error {
	_, err := t.support.UpdateTicketAsStaff(ctx, &supportdto.StaffTicketUpdateCommand{
		TicketId:   id,
		Status:     status,
		FromMirror: inTopic,
	})
	return err
}

// botSubscriptions backs the bot's subscription port with the subscription
// facade. The writes are the use cases the admin panel's endpoints run (row
// lock, column update, cache invalidation), so the bot cannot drift from
// them.
type botSubscriptions struct {
	subscription subscription.Service
}

func (s botSubscriptions) Find(ctx context.Context, id int64) (*usersub.Subscribe, error) {
	return s.subscription.SubscriptionByID(ctx, id)
}

func (s botSubscriptions) ListByUser(ctx context.Context, userID int64) ([]*usersub.SubscribeDetails, error) {
	return s.subscription.UserSubscriptions(ctx, userID)
}

func (s botSubscriptions) ResetTraffic(ctx context.Context, sub *usersub.Subscribe) error {
	return s.subscription.ResetUserSubscribeTraffic(ctx, &subscriptiondto.ResetUserSubscribeTrafficRequest{UserSubscribeId: sub.Id})
}

// SetStatus moves the subscription from the status the operator confirmed
// to status; it fails if the subscription changed in between.
func (s botSubscriptions) SetStatus(ctx context.Context, sub *usersub.Subscribe, status uint8) error {
	return s.subscription.ChangeUserSubscribeStatus(ctx, sub.Id, sub.Status, status)
}

// botBilling backs the bot's billing port with the billing facade.
type botBilling struct {
	billing botBillingReads
}

// botBillingReads is the part of the billing facade the bot reads.
type botBillingReads interface {
	OrderRevenueOn(ctx context.Context, date time.Time) (order.OrdersTotal, error)
	FindWallet(ctx context.Context, userID int64) (*walletEntity.Wallet, error)
}

var _ notification.Billing = botBilling{}

func (b botBilling) Revenue(ctx context.Context, t time.Time) (int64, error) {
	total, err := b.billing.OrderRevenueOn(ctx, t)
	return total.AmountTotal, err
}

func (b botBilling) Balance(ctx context.Context, userID int64) (int64, error) {
	wallet, err := b.billing.FindWallet(ctx, userID)
	if err != nil || wallet == nil {
		return 0, err
	}
	return wallet.Balance, nil
}

// botAuditLogs backs the bot's audit-log port with the platform log
// repository.
type botAuditLogs struct {
	logs repository.LogRepo
}

func (l botAuditLogs) RecentLogins(ctx context.Context, userID int64, limit int) ([]*log.SystemLog, error) {
	entries, _, err := l.logs.FilterSystemLog(ctx, &log.FilterParams{
		Page:     1,
		Size:     limit,
		Type:     log.TypeLogin.Uint8(),
		ObjectID: userID,
	})
	return entries, err
}

func (l botAuditLogs) Insert(ctx context.Context, row *log.SystemLog) error {
	return l.logs.Insert(ctx, row)
}
