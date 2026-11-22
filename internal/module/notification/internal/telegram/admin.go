package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/support/entity/ticket"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/random"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	tgActionTTL    = 5 * time.Minute
	tgActionPrefix = "tg:action:"
)

// staffAuthor is the author the bot records on the ticket follows staff
// write through it; anything but the user marker renders as a staff reply.
const staffAuthor = "admin"

type tgAction struct {
	Cmd     string `json:"cmd"`
	AdminID int64  `json:"admin_id"`
	Target  string `json:"target"`
	Extra   string `json:"extra,omitempty"`
}

// Handle runs an administrator command.
func (a *Admin) Handle(ctx context.Context, msg *models.Message) {
	rawCmd, arg := expandShortcut(messageCommand(msg), commandArguments(msg))

	// Step 1: Admin check
	adminUser, reject := a.authenticate(ctx, msg)
	if reject != "" {
		a.reply(ctx, msg, reject)
		return
	}

	// Step 2: Confirm / cancel short-circuit. A confirmation belongs to the
	// administrator who asked for it: both commands address the sender's own
	// actions, so another administrator's confirmation is never found, let
	// alone cancelled.
	if actionID, ok := strings.CutPrefix(rawCmd, "confirm_"); ok {
		a.confirmAction(ctx, msg, adminUser, actionID)
		return
	}
	if actionID, ok := strings.CutPrefix(rawCmd, "cancel_"); ok {
		if err := a.deps.Actions.Delete(ctx, actionKey(adminUser.Id, actionID)); err != nil {
			logger.WithContext(ctx).Errorw("admin cancel action: redis del failed", logger.Field("error", err.Error()))
		}
		a.reply(ctx, msg, "❌ 操作已取消。")
		return
	}

	// Step 3: Dispatch
	switch rawCmd {
	case "dash":
		a.dashboard(ctx, msg)
	case "tickets":
		page, _ := strconv.Atoi(arg)
		if page < 1 {
			page = 1
		}
		a.listTickets(ctx, msg, page, nil)
	case "tickets_waiting":
		st := uint8(ticket.Pending)
		a.listTickets(ctx, msg, 1, &st)
	case "tk":
		a.ticketDetail(ctx, msg, arg)
	case "rp":
		a.replyTicket(ctx, msg, adminUser, arg)
	case "close":
		a.confirmCloseTicket(ctx, msg, adminUser, arg)
	case "reopen":
		a.reopenTicket(ctx, msg, adminUser, arg)
	case "user":
		a.userDetail(ctx, msg, arg)
	case "user_sub":
		a.userSubs(ctx, msg, arg)
	case "user_log":
		a.userLogs(ctx, msg, arg)
	case "reset":
		a.confirmResetTraffic(ctx, msg, adminUser, arg)
	case "toggle":
		a.confirmToggleSub(ctx, msg, adminUser, arg)
	case "ban":
		a.confirmBanUser(ctx, msg, adminUser, arg)
	case "help", "h":
		a.adminHelp(ctx, msg)
	default:
		a.reply(ctx, msg, "未知命令。/help 查看可用命令。")
	}
}

func (a *Admin) adminHelp(ctx context.Context, msg *models.Message) {
	help := `🤖 Admin Commands

📊 仪表盘
  /dash

🎫 工单
  /tickets [page]    工单列表
  /tickets_waiting   仅待处理
  /tk <id>           详情
  /rp <id> <文本>    回复
  /close <id>        关闭
  /reopen <id>       重新打开

👤 用户
  /user <邮箱|ID>     用户详情
  /user_sub <邮箱|ID> 订阅
  /user_log <邮箱|ID> 登录日志

🔧 操作
  /reset <订阅ID>      重置流量
  /toggle <订阅ID>     启停订阅
  /ban <邮箱|ID>       封/解封用户

/h  或  /help      帮助`
	a.reply(ctx, msg, help)
}

// ─────────────────────────────────────
// Dashboard
// ─────────────────────────────────────

func (a *Admin) dashboard(ctx context.Context, msg *models.Message) {
	now := timeutil.Now()
	log := logger.WithContext(ctx)

	// Each figure is best effort: one failing read shows as zero instead of
	// withholding the whole overview.
	pendingTickets, err := a.deps.Tickets.CountAwaitingReply(ctx)
	if err != nil {
		log.Errorw("dashboard: count pending tickets failed", logger.Field("error", err.Error()))
	}
	todayRevenue, err := a.deps.Billing.Revenue(ctx, now)
	if err != nil {
		log.Errorw("dashboard: revenue failed", logger.Field("error", err.Error()))
	}
	todayUsers, err := a.deps.Accounts.CountRegistrations(ctx, now)
	if err != nil {
		log.Errorw("dashboard: registrations failed", logger.Field("error", err.Error()))
	}
	_, pending, err := a.deps.Tickets.List(ctx, 1, 3, ticketStatusPtr(ticket.Pending))
	if err != nil {
		log.Errorw("dashboard: pending tickets failed", logger.Field("error", err.Error()))
	}
	var recentBlock strings.Builder
	for _, tk := range pending {
		fmt.Fprintf(&recentBlock, "  #%d [%s] %s\n", tk.Id, ticketStatusEmoji(tk.Status), truncate(tk.Title, 30))
	}

	text := fmt.Sprintf(`📊 今日概览  (%s)
━━━━━━━━━━━━━━━━━━
🎫 待处理工单    %d 个
💰 今日收入       ¥%.2f
👤 今日注册       %d 人
━━━━━━━━━━━━━━━━━━`,
		now.Format("01-02 周一"),
		pendingTickets,
		float64(todayRevenue)/100,
		todayUsers,
	)
	if recentBlock.Len() > 0 {
		text += "\n最近待处理工单：\n" + recentBlock.String()
	}
	a.reply(ctx, msg, text)
}

// ─────────────────────────────────────
// Tickets
// ─────────────────────────────────────

func ticketStatusPtr(s uint8) *uint8 { return &s }

func (a *Admin) listTickets(ctx context.Context, msg *models.Message, page int, status *uint8) {
	pageSize := 10
	total, list, err := a.deps.Tickets.List(ctx, page, pageSize, status)
	if err != nil {
		logger.WithContext(ctx).Errorw("list tickets failed", logger.Field("error", err.Error()))
		a.reply(ctx, msg, "查询工单列表失败。")
		return
	}
	if len(list) == 0 {
		a.reply(ctx, msg, "暂无工单。")
		return
	}

	var sb strings.Builder
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages < 1 {
		totalPages = 1
	}
	filterLabel := ""
	if status != nil {
		filterLabel = fmt.Sprintf(" [%s]", ticketStatusName(*status))
	}
	fmt.Fprintf(&sb, "🎫 工单列表%s  (第%d/%d页，共%d单)\n━━━━━━━━━━━━━━━━━━\n", filterLabel, page, totalPages, total)
	for _, tk := range list {
		title := truncate(tk.Title, 28)
		fmt.Fprintf(&sb, "%s #%d %s\n  %s  /tk_%d\n", ticketStatusEmoji(tk.Status), tk.Id, title, tk.CreatedAt.Format("01-02 15:04"), tk.Id)
	}
	sb.WriteString("\n👉 /tk_<id> 查看  /rp_<id> 回复  /close_<id> 关闭\n")
	if page < totalPages {
		fmt.Fprintf(&sb, "📖 下一页：/tickets_%d", page+1)
	}
	a.reply(ctx, msg, sb.String())
}

func (a *Admin) ticketDetail(ctx context.Context, msg *models.Message, idStr string) {
	if idStr == "" {
		a.reply(ctx, msg, "用法：/tk <工单ID>")
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.reply(ctx, msg, "工单ID格式错误。")
		return
	}
	tk, err := a.deps.Tickets.Detail(ctx, id)
	if err != nil {
		logger.WithContext(ctx).Errorw("ticket detail failed", logger.Field("error", err.Error()), logger.Field("id", id))
		a.reply(ctx, msg, "工单不存在或查询失败。")
		return
	}
	email := a.userEmail(ctx, tk.UserId)

	var sb strings.Builder
	fmt.Fprintf(&sb, "🎫 #%d %s\n", tk.Id, ticketStatusName(tk.Status))
	sb.WriteString("━━━━━━━━━━━━━━━━━━\n")
	fmt.Fprintf(&sb, "状态：%s %s\n", ticketStatusEmoji(tk.Status), ticketStatusName(tk.Status))
	fmt.Fprintf(&sb, "用户：%s (ID:%d)\n", email, tk.UserId)
	fmt.Fprintf(&sb, "时间：%s\n", tk.CreatedAt.Format("2006-01-02 15:04"))
	if tk.Description != "" {
		fmt.Fprintf(&sb, "\n描述：%s\n", truncate(tk.Description, 400))
	}
	if len(tk.Follows) > 0 {
		sb.WriteString("\n─── 回复记录 ───\n")
		for _, f := range tk.Follows {
			fromLabel := "用户"
			if !ticket.IsFromUser(f.From) {
				fromLabel = "客服"
			}
			fmt.Fprintf(&sb, "📝 %s (%s)\n   %s\n\n",
				fromLabel,
				f.CreatedAt.Format("01-02 15:04"),
				truncate(f.Content, 300),
			)
		}
	}
	fmt.Fprintf(&sb, "\n👉 /rp_%d <回复>   /close_%d 关闭", tk.Id, tk.Id)
	a.reply(ctx, msg, sb.String())
}

// recordAction writes the audit row of a mutation the administrator made
// through the bot: the bound panel account is the actor, the Telegram sender
// is kept next to it. The mutation is already made; a trail that cannot be
// written is logged.
func (a *Admin) recordAction(ctx context.Context, msg *models.Message, adminUser *user.User, action, object string, objectID int64, detail string) {
	if a.deps.AuditLogs == nil {
		return
	}
	entry := log.AdminAction{Action: action, Object: object, ObjectID: objectID, Detail: detail, Source: log.AdminActionSourceTelegram}
	entry.ActorID = adminUser.Id
	if msg.From != nil {
		entry.TelegramSenderID = msg.From.ID
	}
	row, err := log.NewAdminActionLog(entry)
	if err == nil {
		err = a.deps.AuditLogs.Insert(ctx, row)
	}
	if err != nil {
		logger.WithContext(ctx).Errorw("[Telegram] record admin action failed", logger.Field("error", err.Error()),
			logger.Field("action", action), logger.Field("admin_id", adminUser.Id))
	}
}

func (a *Admin) replyTicket(ctx context.Context, msg *models.Message, adminUser *user.User, args string) {
	parts := strings.SplitN(args, " ", 2)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		a.reply(ctx, msg, "用法：/rp <工单ID> <回复内容>")
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		a.reply(ctx, msg, "工单ID格式错误。")
		return
	}
	previous, err := a.deps.Tickets.Reply(ctx, id, staffAuthor, parts[1], false)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			a.reply(ctx, msg, "工单不存在。")
		case errors.Is(err, ticket.ErrClosed):
			a.reply(ctx, msg, fmt.Sprintf("工单 #%d 已关闭，请先 /reopen_%d 重新打开后再回复。", id, id))
		default:
			logger.WithContext(ctx).Errorw("ticket reply failed", logger.Field("error", err.Error()), logger.Field("ticket_id", id))
			a.reply(ctx, msg, "回复失败，请稍后再试。")
		}
		return
	}
	a.recordAction(ctx, msg, adminUser, "ticket.reply", "ticket", id, "")
	a.reply(ctx, msg, fmt.Sprintf("✅ 已回复工单 #%d\n 状态：%s → 🟡 等待用户回复", id, ticketStatusName(previous)))
}

func (a *Admin) confirmCloseTicket(ctx context.Context, msg *models.Message, adminUser *user.User, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.reply(ctx, msg, "工单ID格式错误。")
		return
	}
	if _, err := a.deps.Tickets.Find(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			a.reply(ctx, msg, "工单不存在。")
			return
		}
		logger.WithContext(ctx).Errorw("close ticket precondition failed", logger.Field("error", err.Error()))
		a.reply(ctx, msg, "查询工单失败。")
		return
	}
	actionID := a.saveAction(ctx, "close", adminUser.Id, strconv.FormatInt(id, 10), "")
	a.reply(ctx, msg, fmt.Sprintf("确认关闭工单 #%d ？\n/confirm_%s 确认\n/cancel_%s 取消", id, actionID, actionID))
}

func (a *Admin) reopenTicket(ctx context.Context, msg *models.Message, adminUser *user.User, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.reply(ctx, msg, "ID格式错误。")
		return
	}
	if err := a.deps.Tickets.SetStatus(ctx, id, ticket.Pending, false); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			a.reply(ctx, msg, "工单不存在。")
			return
		}
		logger.WithContext(ctx).Errorw("reopen ticket failed", logger.Field("error", err.Error()))
		a.reply(ctx, msg, "操作失败。")
		return
	}
	a.recordAction(ctx, msg, adminUser, "ticket.status", "ticket", id, fmt.Sprintf("status=%d", ticket.Pending))
	a.reply(ctx, msg, fmt.Sprintf("✅ 工单 #%d 已重新打开", id))
}

// ─────────────────────────────────────
// User
// ─────────────────────────────────────

func (a *Admin) lookupUser(ctx context.Context, msg *models.Message, input string) (*user.User, bool) {
	if input == "" {
		a.reply(ctx, msg, "用法：/user <邮箱|ID>")
		return nil, false
	}
	if id, e := strconv.ParseInt(input, 10, 64); e == nil {
		u, err := a.deps.Accounts.FindUser(ctx, id)
		if err == nil && u.Id > 0 {
			return u, true
		}
	}
	auth, err := a.deps.Accounts.FindBinding(ctx, "email", input)
	if err == nil && auth.UserId > 0 {
		u, err := a.deps.Accounts.FindUser(ctx, auth.UserId)
		if err == nil {
			return u, true
		}
	}
	a.reply(ctx, msg, "找不到用户。")
	return nil, false
}

// authenticate resolves the command sender. Inside the admin group the chat
// id is the group, so identity always comes from the From field: the sender
// must have their own Telegram bound to a panel administrator account.
func (a *Admin) authenticate(ctx context.Context, msg *models.Message) (admin *user.User, rejectMsg string) {
	if msg.From == nil || msg.From.IsBot {
		return nil, "无法识别命令发送者。"
	}
	senderID := strconv.FormatInt(msg.From.ID, 10)
	log := logger.WithContext(ctx)

	auth, err := a.deps.Accounts.FindBinding(ctx, "telegram", senderID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Infow("admin auth: Telegram not bound", logger.Field("sender_id", msg.From.ID))
			return nil, "您的 Telegram 尚未绑定账号。\n请登录 Web 后台 → 个人设置 → 绑定 Telegram。"
		}
		log.Errorw("admin auth: query auth method failed", logger.Field("error", err.Error()))
		return nil, "系统错误，请稍后再试。"
	}

	u, err := a.deps.Accounts.FindUser(ctx, auth.UserId)
	if err != nil {
		log.Errorw("admin auth: query user failed", logger.Field("error", err.Error()), logger.Field("user_id", auth.UserId))
		return nil, "系统错误，请稍后再试。"
	}
	if refusal := panelAdminRefusal(u); refusal != "" {
		log.Infow("admin auth: sender may not administer", logger.Field("user_id", u.Id), logger.Field("reason", refusal))
		return nil, "您没有管理权限。"
	}
	return u, ""
}

// accountRefusal explains why an account may not use the bot, or returns ""
// when it may. FindUser is unscoped, so a soft-deleted account comes back
// like any other; it is refused here together with disabled ones, the same
// gates the HTTP routes apply.
func accountRefusal(u *user.User) string {
	switch {
	case u.DeletedAt.Valid:
		return "account deleted"
	case u.Enable == nil || !*u.Enable:
		return "account disabled"
	}
	return ""
}

// panelAdminRefusal explains why the account bound to a Telegram sender may
// not act as a panel administrator, or returns "" when it may.
func panelAdminRefusal(u *user.User) string {
	if refusal := accountRefusal(u); refusal != "" {
		return refusal
	}
	if u.IsAdmin == nil || !*u.IsAdmin {
		return "not an administrator"
	}
	return ""
}

// accountDisabled reports whether /ban finds the account switched off. Only
// an explicit false counts: the column is NOT NULL and defaults to on.
func accountDisabled(u *user.User) bool {
	return u.Enable != nil && !*u.Enable
}

// enabledLabel names an account switch state for staff.
func enabledLabel(enabled bool) string {
	if enabled {
		return "启用"
	}
	return "禁用"
}

// userEmail labels a user for staff: the bound email, or the numeric id.
func (a *Admin) userEmail(ctx context.Context, userID int64) string {
	auths, err := a.deps.Accounts.ListBindings(ctx, userID)
	if err != nil {
		logger.WithContext(ctx).Errorw("list user bindings failed", logger.Field("error", err.Error()), logger.Field("user_id", userID))
	}
	for _, auth := range auths {
		if auth.AuthType == "email" {
			return auth.AuthIdentifier
		}
	}
	return fmt.Sprintf("ID:%d", userID)
}

// userSubscriptions lists a user's subscriptions for display; a failed read
// shows as none.
func (a *Admin) userSubscriptions(ctx context.Context, userID int64) []*usersub.SubscribeDetails {
	subs, err := a.deps.Subscriptions.ListByUser(ctx, userID)
	if err != nil {
		logger.WithContext(ctx).Errorw("list user subscriptions failed", logger.Field("error", err.Error()), logger.Field("user_id", userID))
	}
	return subs
}

func (a *Admin) userDetail(ctx context.Context, msg *models.Message, input string) {
	u, ok := a.lookupUser(ctx, msg, input)
	if !ok {
		return
	}
	subs := a.userSubscriptions(ctx, u.Id)

	enable := "❌ 已禁用"
	if u.Enable != nil && *u.Enable {
		enable = "✅ 启用"
	}
	adminFlag := "普通"
	if u.IsAdmin != nil && *u.IsAdmin {
		adminFlag = "⭐ 管理员"
	}
	email := a.userEmail(ctx, u.Id)
	balance, err := a.deps.Billing.Balance(ctx, u.Id)
	if err != nil {
		logger.WithContext(ctx).Errorw("user balance failed", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "👤 用户详情\n━━━━━━━━━━━━━━━━━━\nID：%d\n邮箱：%s\n状态：%s\n角色：%s\n余额：¥%.2f\n注册：%s\n推荐码：%s\n",
		u.Id, email, enable, adminFlag,
		float64(balance)/100,
		u.CreatedAt.Format("2006-01-02"),
		u.ReferCode,
	)

	auths, err := a.deps.Accounts.ListBindings(ctx, u.Id)
	if err != nil {
		logger.WithContext(ctx).Errorw("list user bindings failed", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
	}
	if len(auths) > 0 {
		sb.WriteString("\n绑定方式：\n")
		for _, auth := range auths {
			fmt.Fprintf(&sb, "  • %s\n", auth.AuthType)
		}
	}

	if len(subs) > 0 {
		sb.WriteString("\n─── 当前订阅 ───\n")
		for _, s := range subs {
			fmt.Fprintf(&sb, "📦 %s (ID:%d)\n   流量：%.1f/%.1fGB  到期：%s\n\n",
				planName(s), s.Id, gigabytes(s.Download+s.Upload), gigabytes(s.Traffic), expiryLabel(s.ExpireTime, timeutil.Now()),
			)
		}
	}
	sb.WriteString("━━━━━━━━━━━━━━━━━━\n📌 快捷操作：\n")
	for _, s := range subs {
		fmt.Fprintf(&sb, "  /reset_%d 重置  /toggle_%d 启停\n", s.Id, s.Id)
	}
	fmt.Fprintf(&sb, "\n  /user_sub_%d  /user_log_%d  /ban_%d %s",
		u.Id, u.Id, u.Id, enabledLabel(accountDisabled(u)),
	)

	a.reply(ctx, msg, sb.String())
}

func (a *Admin) userSubs(ctx context.Context, msg *models.Message, input string) {
	u, ok := a.lookupUser(ctx, msg, input)
	if !ok {
		return
	}
	subs := a.userSubscriptions(ctx, u.Id)
	if len(subs) == 0 {
		a.reply(ctx, msg, "用户无订阅。")
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "📦 用户 %s 订阅列表 (%d)\n", a.userEmail(ctx, u.Id), len(subs))
	for i, s := range subs {
		expiry := "无限期"
		if !usersub.NoExpiry(s.ExpireTime) {
			expiry = s.ExpireTime.In(timeutil.Location()).Format("2006-01-02 15:04")
		}
		fmt.Fprintf(&sb, "\n%d. %s (ID:%d)\n   %s\n   到期：%s\n", i+1, planName(s), s.Id, subStatusName(s.Status), expiry)
	}
	a.reply(ctx, msg, sb.String())
}

// expiryLabel names a subscription's term end for staff: the date with the
// days left and a warning when it is near, or "no time limit" for a
// subscription without one (usersub.NoExpiry), which used to show as a
// negative number of days about to expire.
func expiryLabel(expireTime, now time.Time) string {
	if usersub.NoExpiry(expireTime) {
		return "无限期"
	}
	daysLeft := int(expireTime.Sub(now).Hours() / 24)
	label := fmt.Sprintf("%s (剩%d天)", expireTime.In(timeutil.Location()).Format("2006-01-02"), daysLeft)
	if daysLeft <= 3 {
		label += " ⚠️即将过期"
	}
	return label
}

func (a *Admin) userLogs(ctx context.Context, msg *models.Message, input string) {
	u, ok := a.lookupUser(ctx, msg, input)
	if !ok {
		return
	}
	email := a.userEmail(ctx, u.Id)
	logs, err := a.deps.AuditLogs.RecentLogins(ctx, u.Id, 10)
	if err != nil {
		logger.WithContext(ctx).Errorw("user logs failed", logger.Field("error", err.Error()))
		a.reply(ctx, msg, "查询日志失败。")
		return
	}
	if len(logs) == 0 {
		a.reply(ctx, msg, fmt.Sprintf("📜 %s 无登录日志。", email))
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "📜 %s 最近登录 (最多10)\n", email)
	for _, entry := range logs {
		var entryLog log.Login
		if err := entryLog.Unmarshal([]byte(entry.Content)); err != nil {
			continue
		}
		marker := "❌"
		if entryLog.Success {
			marker = "✅"
		}
		fmt.Fprintf(&sb, "%s %s  %s  %s\n",
			marker, entry.CreatedAt.Format("01-02 15:04"),
			entryLog.LoginIP, entryLog.Method,
		)
	}
	a.reply(ctx, msg, sb.String())
}

// ─────────────────────────────────────
// Mutations (with confirm)
// ─────────────────────────────────────

func (a *Admin) confirmResetTraffic(ctx context.Context, msg *models.Message, adminUser *user.User, idStr string) {
	subID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.reply(ctx, msg, "订阅ID格式错误。")
		return
	}
	sub, err := a.findSubscription(ctx, subID)
	if err != nil {
		a.reply(ctx, msg, "订阅不存在。")
		return
	}
	actionID := a.saveAction(ctx, "reset", adminUser.Id, strconv.FormatInt(subID, 10), sub.Token)
	usedStr := trafficGB(sub.Download + sub.Upload)
	a.reply(ctx, msg, fmt.Sprintf("确认重置 订阅(ID:%d)流量？\n  已用：%s\n\n/confirm_%s 确认\n/cancel_%s 取消",
		subID, usedStr, actionID, actionID))
}

// toggleTarget returns the status /toggle moves a subscription to: an active
// subscription is paused and a paused one resumed. No other status is the
// command's to change. Resuming a finished, expired or refunded (deducted)
// subscription would hand back service its lifecycle already ended.
func toggleTarget(status uint8) (uint8, bool) {
	switch status {
	case usersub.SubscribeStatusActive:
		return usersub.SubscribeStatusStopped, true
	case usersub.SubscribeStatusStopped:
		return usersub.SubscribeStatusActive, true
	}
	return 0, false
}

func toggleRefusal(subID int64, status uint8) string {
	return fmt.Sprintf("订阅 (ID:%d) 当前状态为 %s，只有活跃或已暂停的订阅可以启停。", subID, subStatusName(status))
}

func (a *Admin) confirmToggleSub(ctx context.Context, msg *models.Message, adminUser *user.User, idStr string) {
	subID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.reply(ctx, msg, "订阅ID格式错误。")
		return
	}
	userSub, err := a.findSubscription(ctx, subID)
	if err != nil {
		a.reply(ctx, msg, "订阅不存在。")
		return
	}
	target, ok := toggleTarget(userSub.Status)
	if !ok {
		a.reply(ctx, msg, toggleRefusal(subID, userSub.Status))
		return
	}
	opLabel := "暂停"
	if target == usersub.SubscribeStatusActive {
		opLabel = "启用"
	}
	// The confirmation carries the status the prompt was worded for, so it
	// cannot apply the opposite change if the subscription moved meanwhile.
	actionID := a.saveAction(ctx, "toggle", adminUser.Id, strconv.FormatInt(subID, 10), strconv.Itoa(int(userSub.Status)))
	a.reply(ctx, msg, fmt.Sprintf("确认%s订阅 (ID:%d) ？\n/confirm_%s 确认\n/cancel_%s 取消",
		opLabel, subID, actionID, actionID))
}

func (a *Admin) confirmBanUser(ctx context.Context, msg *models.Message, adminUser *user.User, input string) {
	u, ok := a.lookupUser(ctx, msg, input)
	if !ok {
		return
	}
	if u.Id == adminUser.Id {
		a.reply(ctx, msg, "无法对自己的账号执行此操作。")
		return
	}
	// The confirmation carries the state the prompt was worded for, so it
	// cannot apply the opposite change if the account was switched meanwhile.
	target := accountDisabled(u)
	actionID := a.saveAction(ctx, "ban", adminUser.Id, strconv.FormatInt(u.Id, 10), strconv.FormatBool(target))
	a.reply(ctx, msg, fmt.Sprintf("确认%s用户 %s (ID:%d) ？\n/confirm_%s 确认\n/cancel_%s 取消",
		enabledLabel(target), a.userEmail(ctx, u.Id), u.Id, actionID, actionID))
}

func (a *Admin) confirmAction(ctx context.Context, msg *models.Message, adminUser *user.User, actionID string) {
	// The action is consumed before it is applied, in one step: two
	// confirmations racing each other must not both find it, or the first
	// would switch the account and the second switch it back.
	act, ok := a.takeAction(ctx, actionID, adminUser.Id)
	if !ok {
		a.reply(ctx, msg, "操作已过期或无效。")
		return
	}
	id, _ := strconv.ParseInt(act.Target, 10, 64)
	settled := true
	switch act.Cmd {
	case "close":
		settled = a.closeTicket(ctx, msg, adminUser, id)
	case "reset":
		settled = a.resetTraffic(ctx, msg, adminUser, id)
	case "toggle":
		settled = a.toggleSubscription(ctx, msg, adminUser, id, act.Extra)
	case "ban":
		settled = a.toggleBan(ctx, msg, adminUser, id, act.Extra)
	default:
		a.reply(ctx, msg, "未知操作。")
	}
	if !settled {
		// A confirmation that failed stays redeemable, so the administrator
		// can retry it.
		a.storeAction(ctx, adminUser.Id, actionID, act)
	}
}

func (a *Admin) closeTicket(ctx context.Context, msg *models.Message, adminUser *user.User, id int64) bool {
	if err := a.deps.Tickets.SetStatus(ctx, id, ticket.Closed, false); err != nil {
		logger.WithContext(ctx).Errorw("close ticket failed", logger.Field("error", err.Error()))
		a.reply(ctx, msg, "关闭工单失败。")
		return false
	}
	a.recordAction(ctx, msg, adminUser, "ticket.status", "ticket", id, fmt.Sprintf("status=%d", ticket.Closed))
	a.reply(ctx, msg, fmt.Sprintf("✅ 工单 #%d 已关闭", id))
	return true
}

func (a *Admin) resetTraffic(ctx context.Context, msg *models.Message, adminUser *user.User, id int64) bool {
	userSub, err := a.findSubscription(ctx, id)
	if err != nil {
		a.reply(ctx, msg, "订阅不存在。")
		return false
	}
	if err := a.deps.Subscriptions.ResetTraffic(ctx, userSub); err != nil {
		logger.WithContext(ctx).Errorw("reset traffic failed", logger.Field("error", err.Error()))
		a.reply(ctx, msg, "重置流量失败。")
		return false
	}
	a.recordAction(ctx, msg, adminUser, "subscription.reset_traffic", "user_subscribe", id, fmt.Sprintf("user_id=%d", userSub.UserId))
	a.reply(ctx, msg, fmt.Sprintf("✅ 订阅 ID:%d 流量已重置", id))
	return true
}

// toggleSubscription applies a confirmed /toggle. promptedStatus is the
// status the confirmation prompt was worded for; it is empty for
// confirmations issued before the prompt recorded it.
func (a *Admin) toggleSubscription(ctx context.Context, msg *models.Message, adminUser *user.User, id int64, promptedStatus string) bool {
	userSub, err := a.findSubscription(ctx, id)
	if err != nil {
		a.reply(ctx, msg, "订阅不存在。")
		return false
	}
	target, ok := toggleTarget(userSub.Status)
	if !ok {
		a.reply(ctx, msg, toggleRefusal(id, userSub.Status))
		return true
	}
	if promptedStatus != "" && promptedStatus != strconv.Itoa(int(userSub.Status)) {
		a.reply(ctx, msg, fmt.Sprintf("订阅 (ID:%d) 的状态已变为 %s，请重新执行 /toggle。", id, subStatusName(userSub.Status)))
		return true
	}
	if err := a.deps.Subscriptions.SetStatus(ctx, userSub, target); err != nil {
		logger.WithContext(ctx).Errorw("toggle sub failed", logger.Field("error", err.Error()))
		a.reply(ctx, msg, "操作失败。")
		return false
	}
	a.recordAction(ctx, msg, adminUser, "subscription.status", "user_subscribe", id, fmt.Sprintf("user_id=%d status=%d", userSub.UserId, target))
	opLabel := "已暂停"
	if target == usersub.SubscribeStatusActive {
		opLabel = "已启用"
	}
	a.reply(ctx, msg, fmt.Sprintf("✅ 订阅 ID:%d %s", id, opLabel))
	return true
}

// toggleBan applies a confirmed /ban. promptedTarget is the switch state the
// confirmation prompt announced ("true" enables, "false" disables); it is
// empty for confirmations issued before the prompt recorded it, which switch
// the state found now.
func (a *Admin) toggleBan(ctx context.Context, msg *models.Message, adminUser *user.User, id int64, promptedTarget string) bool {
	u, err := a.deps.Accounts.FindUser(ctx, id)
	if err != nil {
		a.reply(ctx, msg, "用户不存在。")
		return false
	}
	enabled := !accountDisabled(u)
	target := !enabled
	if promptedTarget != "" {
		target = promptedTarget == "true"
		if target == enabled {
			// The account already is where the prompt would move it: it was
			// switched meanwhile, so there is nothing left to confirm and the
			// account must not be switched back.
			a.reply(ctx, msg, fmt.Sprintf("用户 (ID:%d) 已处于%s状态，本次操作未执行。", u.Id, enabledLabel(enabled)))
			return true
		}
	}
	// Only the flag: a full-row save from this lookup could revert a
	// concurrent change to the account.
	if err := a.deps.Accounts.SetEnabled(ctx, u.Id, target); err != nil {
		logger.WithContext(ctx).Errorw("ban user failed", logger.Field("error", err.Error()))
		a.reply(ctx, msg, "操作失败。")
		return false
	}
	a.recordAction(ctx, msg, adminUser, "user.ban", "user", u.Id, fmt.Sprintf("enabled=%t", target))
	a.reply(ctx, msg, fmt.Sprintf("✅ 用户 (ID:%d) 已%s", u.Id, enabledLabel(target)))
	return true
}

// findSubscription loads a subscription a command targets. Callers answer
// every failure as "not found"; anything but a miss is logged.
func (a *Admin) findSubscription(ctx context.Context, id int64) (*usersub.Subscribe, error) {
	sub, err := a.deps.Subscriptions.Find(ctx, id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.WithContext(ctx).Errorw("find subscription failed", logger.Field("error", err.Error()), logger.Field("subscription_id", id))
	}
	return sub, err
}

// ─────────────────────────────────────
// Action token (Redis)
// ─────────────────────────────────────

// actionKey addresses a pending action of the administrator adminID. The
// key carries the issuer, so a confirmation or cancellation by anyone else
// looks for a key that does not exist: the issuer's action is neither
// consumed, nor cancelled, nor re-stored with a fresh confirmation window.
func actionKey(adminID int64, actionID string) string {
	return fmt.Sprintf("%s%d:%s", tgActionPrefix, adminID, actionID)
}

func (a *Admin) saveAction(ctx context.Context, cmd string, adminID int64, target, extra string) string {
	actionID := random.KeyNew(8, 1)
	a.storeAction(ctx, adminID, actionID, tgAction{Cmd: cmd, AdminID: adminID, Target: target, Extra: extra})
	return actionID
}

// storeAction keeps act redeemable as actionID by adminID for one
// confirmation window. A failed write is only logged: the prompt still goes
// out, and its confirmation will report itself expired.
func (a *Admin) storeAction(ctx context.Context, adminID int64, actionID string, act tgAction) {
	data, err := json.Marshal(&act)
	if err == nil {
		err = a.deps.Actions.Set(ctx, actionKey(adminID, actionID), string(data), tgActionTTL)
	}
	if err != nil {
		logger.WithContext(ctx).Errorw("save admin action failed", logger.Field("error", err.Error()), logger.Field("cmd", act.Cmd))
	}
}

// takeAction consumes the pending action actionID of adminID. The read and
// the delete are one Redis command, so concurrent confirmations cannot both
// receive the action; another administrator's action lives under another
// key and reads as missing.
func (a *Admin) takeAction(ctx context.Context, actionID string, adminID int64) (tgAction, bool) {
	val, err := a.deps.Actions.GetDel(ctx, actionKey(adminID, actionID))
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			logger.WithContext(ctx).Errorw("load action failed", logger.Field("error", err.Error()))
		}
		return tgAction{}, false
	}
	var act tgAction
	if err := json.Unmarshal([]byte(val), &act); err != nil || act.AdminID != adminID {
		return tgAction{}, false
	}
	return act, true
}

// ─────────────────────────────────────
// Display helpers
// ─────────────────────────────────────

func ticketStatusName(s uint8) string {
	switch s {
	case ticket.Pending:
		return "待处理"
	case ticket.Waiting:
		return "等待用户回复"
	case ticket.Processed:
		return "已处理"
	case ticket.Closed:
		return "已关闭"
	}
	return fmt.Sprintf("状态%d", s)
}

func ticketStatusEmoji(s uint8) string {
	switch s {
	case ticket.Pending:
		return "🔴"
	case ticket.Waiting:
		return "🟡"
	case ticket.Processed:
		return "🟢"
	case ticket.Closed:
		return "⚪"
	}
	return "❔"
}

func subStatusName(s uint8) string {
	switch s {
	case usersub.SubscribeStatusPending:
		return "⏳ 待激活"
	case usersub.SubscribeStatusActive:
		return "✅ 活跃"
	case usersub.SubscribeStatusFinished:
		return "🟢 已完成"
	case usersub.SubscribeStatusExpired:
		return "⚪ 已过期"
	case usersub.SubscribeStatusDeducted:
		return "💸 已扣量"
	case usersub.SubscribeStatusStopped:
		return "🛑 已暂停"
	}
	return fmt.Sprintf("状态%d", s)
}

// planName names a subscription's plan for display; the plan association
// may be missing.
func planName(s *usersub.SubscribeDetails) string {
	if s.Subscribe == nil {
		return ""
	}
	return s.Subscribe.Name
}

func gigabytes(bytes int64) float64 {
	return float64(bytes) / (1024 * 1024 * 1024)
}

func trafficGB(bytes int64) string {
	gb := gigabytes(bytes)
	if gb >= 1 {
		return fmt.Sprintf("%.1fGB", gb)
	}
	mb := float64(bytes) / (1024 * 1024)
	return fmt.Sprintf("%.0fMB", mb)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
