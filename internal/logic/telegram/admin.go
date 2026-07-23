package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/model/entity/ticket"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/random"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	tgActionTTL    = 5 * time.Minute
	tgActionPrefix = "tg:action:"
)

type tgAction struct {
	Cmd     string `json:"cmd"`
	AdminID int64  `json:"admin_id"`
	Target  string `json:"target"`
	Extra   string `json:"extra,omitempty"`
}

// admin runs an admin command; returns true if the message was consumed.
func (l *TelegramLogic) admin(msg *tgbotapi.Message) {
	rawCmd := msg.Command()
	arg := msg.CommandArguments()

	// Step 1: Admin check
	adminUser, reject := adminAuth(l.ctx, l.svcCtx, msg)
	if reject != "" {
		_ = l.sendMessage(l.svcCtx.TelegramBot, reject, msg.Chat.ID)
		return
	}

	// Step 2: Confirm / cancel short-circuit
	if strings.HasPrefix(rawCmd, "confirm_") {
		l.confirmAction(msg, adminUser, strings.TrimPrefix(rawCmd, "confirm_"))
		return
	}
	if strings.HasPrefix(rawCmd, "cancel_") {
		actionID := strings.TrimPrefix(rawCmd, "cancel_")
		if err := l.svcCtx.Redis.Del(l.ctx, tgActionPrefix+actionID).Err(); err != nil {
			l.Errorw("admin cancel action: redis del failed", logger.Field("error", err.Error()))
		}
		_ = l.sendMessage(l.svcCtx.TelegramBot, "❌ 操作已取消。", msg.Chat.ID)
		return
	}

	// Step 3: Dispatch
	switch rawCmd {
	case "dash":
		l.dashboard(msg, adminUser)
	case "tickets":
		page, _ := strconv.Atoi(arg)
		if page < 1 {
			page = 1
		}
		l.listTickets(msg, adminUser, page, nil)
	case "tickets_waiting":
		st := uint8(ticket.Pending)
		l.listTickets(msg, adminUser, 1, &st)
	case "tk":
		l.ticketDetail(msg, adminUser, arg)
	case "rp":
		l.replyTicket(msg, adminUser, arg)
	case "close":
		l.confirmCloseTicket(msg, adminUser, arg)
	case "reopen":
		l.reopenTicket(msg, adminUser, arg)
	case "user":
		l.userDetail(msg, adminUser, arg)
	case "user_sub":
		l.userSubs(msg, adminUser, arg)
	case "user_log":
		l.userLogs(msg, adminUser, arg)
	case "reset":
		l.confirmResetTraffic(msg, adminUser, arg)
	case "toggle":
		l.confirmToggleSub(msg, adminUser, arg)
	case "ban":
		l.confirmBanUser(msg, adminUser, arg)
	case "help", "h":
		l.adminHelp(msg)
	default:
		_ = l.sendMessage(l.svcCtx.TelegramBot, "未知命令。/help 查看可用命令。", msg.Chat.ID)
	}
}

func (l *TelegramLogic) adminHelp(msg *tgbotapi.Message) {
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
	_ = l.sendMessage(l.svcCtx.TelegramBot, help, msg.Chat.ID)
}

// ─────────────────────────────────────
// Dashboard
// ─────────────────────────────────────

func (l *TelegramLogic) dashboard(msg *tgbotapi.Message, adminUser *user.User) {
	ctx := l.ctx
	now := timeutil.Now()

	pendingTickets, _ := l.svcCtx.Store.Ticket().QueryWaitReplyTotal(ctx)
	orderData, _ := l.svcCtx.Store.Order().QueryDateOrders(ctx, now)
	todayRevenue := orderData.AmountTotal
	todayUsers, _ := l.svcCtx.Store.User().QueryResisterUserTotalByDate(ctx, now)

	_, pending, _ := l.svcCtx.Store.Ticket().QueryTicketList(ctx, 1, 3, 0, ticketStatusPtr(ticket.Pending), "")
	var recentBlock strings.Builder
	for _, tk := range pending {
		recentBlock.WriteString(fmt.Sprintf("  #%d [%s] %s\n", tk.Id, ticketStatusEmoji(tk.Status), truncate(tk.Title, 30)))
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
	_ = l.sendMessage(l.svcCtx.TelegramBot, text, msg.Chat.ID)
}

// ─────────────────────────────────────
// Tickets
// ─────────────────────────────────────

func ticketStatusPtr(s uint8) *uint8 { return &s }

func (l *TelegramLogic) listTickets(msg *tgbotapi.Message, adminUser *user.User, page int, status *uint8) {
	pageSize := 10
	total, list, err := l.svcCtx.Store.Ticket().QueryTicketList(l.ctx, page, pageSize, 0, status, "")
	if err != nil {
		l.Errorw("list tickets failed", logger.Field("error", err.Error()))
		_ = l.sendMessage(l.svcCtx.TelegramBot, "查询工单列表失败。", msg.Chat.ID)
		return
	}
	if len(list) == 0 {
		_ = l.sendMessage(l.svcCtx.TelegramBot, "暂无工单。", msg.Chat.ID)
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
	sb.WriteString(fmt.Sprintf("🎫 工单列表%s  (第%d/%d页，共%d单)\n━━━━━━━━━━━━━━━━━━\n", filterLabel, page, totalPages, total))
	for _, tk := range list {
		title := truncate(tk.Title, 28)
		sb.WriteString(fmt.Sprintf("%s #%d %s\n  %s  /tk_%d\n", ticketStatusEmoji(tk.Status), tk.Id, title, tk.CreatedAt.Format("01-02 15:04"), tk.Id))
	}
	sb.WriteString("\n👉 /tk_<id> 查看  /rp_<id> 回复  /close_<id> 关闭\n")
	if page < totalPages {
		sb.WriteString(fmt.Sprintf("📖 下一页：/tickets_%d", page+1))
	}
	_ = l.sendMessage(l.svcCtx.TelegramBot, sb.String(), msg.Chat.ID)
}

func (l *TelegramLogic) ticketDetail(msg *tgbotapi.Message, adminUser *user.User, idStr string) {
	if idStr == "" {
		_ = l.sendMessage(l.svcCtx.TelegramBot, "用法：/tk <工单ID>", msg.Chat.ID)
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		_ = l.sendMessage(l.svcCtx.TelegramBot, "工单ID格式错误。", msg.Chat.ID)
		return
	}
	tk, err := l.svcCtx.Store.Ticket().QueryTicketDetail(l.ctx, id)
	if err != nil {
		l.Errorw("ticket detail failed", logger.Field("error", err.Error()), logger.Field("id", id))
		_ = l.sendMessage(l.svcCtx.TelegramBot, "工单不存在或查询失败。", msg.Chat.ID)
		return
	}
	email, _ := userEmail(l.ctx, l.svcCtx, tk.UserId)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🎫 #%d %s\n", tk.Id, ticketStatusName(tk.Status)))
	sb.WriteString("━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString(fmt.Sprintf("状态：%s %s\n", ticketStatusEmoji(tk.Status), ticketStatusName(tk.Status)))
	sb.WriteString(fmt.Sprintf("用户：%s (ID:%d)\n", email, tk.UserId))
	sb.WriteString(fmt.Sprintf("时间：%s\n", tk.CreatedAt.Format("2006-01-02 15:04")))
	if tk.Description != "" {
		sb.WriteString(fmt.Sprintf("\n描述：%s\n", truncate(tk.Description, 400)))
	}
	if len(tk.Follows) > 0 {
		sb.WriteString("\n─── 回复记录 ───\n")
		for _, f := range tk.Follows {
			fromLabel := "用户"
			if f.From != "user" && f.From != "" {
				fromLabel = "客服"
			}
			sb.WriteString(fmt.Sprintf("📝 %s (%s)\n   %s\n\n",
				fromLabel,
				f.CreatedAt.Format("01-02 15:04"),
				truncate(f.Content, 300),
			))
		}
	}
	sb.WriteString(fmt.Sprintf("\n👉 /rp_%d <回复>   /close_%d 关闭", tk.Id, tk.Id))
	_ = l.sendMessage(l.svcCtx.TelegramBot, sb.String(), msg.Chat.ID)
}

func (l *TelegramLogic) replyTicket(msg *tgbotapi.Message, adminUser *user.User, args string) {
	parts := strings.SplitN(args, " ", 2)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		_ = l.sendMessage(l.svcCtx.TelegramBot, "用法：/rp <工单ID> <回复内容>", msg.Chat.ID)
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		_ = l.sendMessage(l.svcCtx.TelegramBot, "工单ID格式错误。", msg.Chat.ID)
		return
	}
	tk, err := l.svcCtx.Store.Ticket().FindOne(l.ctx, id)
	if err != nil {
		_ = l.sendMessage(l.svcCtx.TelegramBot, "工单不存在。", msg.Chat.ID)
		return
	}
	follow := &ticket.Follow{
		TicketId: id,
		From:     "admin",
		Type:     1,
		Content:  parts[1],
	}
	if err := l.svcCtx.Store.Ticket().InsertTicketFollow(l.ctx, follow); err != nil {
		l.Errorw("ticket follow insert failed", logger.Field("error", err.Error()))
		_ = l.sendMessage(l.svcCtx.TelegramBot, "回复失败，请稍后再试。", msg.Chat.ID)
		return
	}
	if err := l.svcCtx.Store.Ticket().UpdateTicketStatus(l.ctx, id, 0, ticket.Waiting); err != nil {
		l.Errorw("ticket status update failed", logger.Field("error", err.Error()))
	}
	_ = l.sendMessage(l.svcCtx.TelegramBot, fmt.Sprintf("✅ 已回复工单 #%d\n 状态：%s → 🟡 等待用户回复", id, ticketStatusName(tk.Status)), msg.Chat.ID)
}

func (l *TelegramLogic) confirmCloseTicket(msg *tgbotapi.Message, adminUser *user.User, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		_ = l.sendMessage(l.svcCtx.TelegramBot, "工单ID格式错误。", msg.Chat.ID)
		return
	}
	if _, err := l.svcCtx.Store.Ticket().FindOne(l.ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = l.sendMessage(l.svcCtx.TelegramBot, "工单不存在。", msg.Chat.ID)
			return
		}
		l.Errorw("close ticket precondition failed", logger.Field("error", err.Error()))
		_ = l.sendMessage(l.svcCtx.TelegramBot, "查询工单失败。", msg.Chat.ID)
		return
	}
	actionID := l.saveAction("close", adminUser.Id, strconv.FormatInt(id, 10), "")
	_ = l.sendMessage(l.svcCtx.TelegramBot,
		fmt.Sprintf("确认关闭工单 #%d ？\n/confirm_%s 确认\n/cancel_%s 取消", id, actionID, actionID),
		msg.Chat.ID)
}

func (l *TelegramLogic) reopenTicket(msg *tgbotapi.Message, adminUser *user.User, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		_ = l.sendMessage(l.svcCtx.TelegramBot, "ID格式错误。", msg.Chat.ID)
		return
	}
	if err := l.svcCtx.Store.Ticket().UpdateTicketStatus(l.ctx, id, 0, ticket.Pending); err != nil {
		l.Errorw("reopen ticket failed", logger.Field("error", err.Error()))
		_ = l.sendMessage(l.svcCtx.TelegramBot, "操作失败。", msg.Chat.ID)
		return
	}
	_ = l.sendMessage(l.svcCtx.TelegramBot, fmt.Sprintf("✅ 工单 #%d 已重新打开", id), msg.Chat.ID)
}

// ─────────────────────────────────────
// User
// ─────────────────────────────────────

func (l *TelegramLogic) lookupUser(msg *tgbotapi.Message, input string) (*user.User, bool) {
	if input == "" {
		_ = l.sendMessage(l.svcCtx.TelegramBot, "用法：/user <邮箱|ID>", msg.Chat.ID)
		return nil, false
	}
	if id, e := strconv.ParseInt(input, 10, 64); e == nil {
		u, err := l.svcCtx.Store.User().FindOne(l.ctx, id)
		if err == nil && u.Id > 0 {
			return u, true
		}
	}
	auth, err := l.svcCtx.Store.UserAuth().FindUserAuthMethodByOpenID(l.ctx, "email", input)
	if err == nil && auth.UserId > 0 {
		u, err := l.svcCtx.Store.User().FindOne(l.ctx, auth.UserId)
		if err == nil {
			return u, true
		}
	}
	_ = l.sendMessage(l.svcCtx.TelegramBot, "找不到用户。", msg.Chat.ID)
	return nil, false
}

func userEmail(ctx context.Context, svcCtx *svc.ServiceContext, userId int64) (string, error) {
	auths, err := svcCtx.Store.UserAuth().FindUserAuthMethods(ctx, userId)
	if err != nil {
		return fmt.Sprintf("ID:%d", userId), err
	}
	for _, a := range auths {
		if a.AuthType == "email" {
			return a.AuthIdentifier, nil
		}
	}
	return fmt.Sprintf("ID:%d", userId), nil
}

func (l *TelegramLogic) userDetail(msg *tgbotapi.Message, adminUser *user.User, input string) {
	u, ok := l.lookupUser(msg, input)
	if !ok {
		return
	}
	subs, _ := l.svcCtx.Store.UserSubscription().QueryUserSubscribe(l.ctx, u.Id)

	enable := "❌ 已禁用"
	if u.Enable != nil && *u.Enable {
		enable = "✅ 启用"
	}
	adminFlag := "普通"
	if u.IsAdmin != nil && *u.IsAdmin {
		adminFlag = "⭐ 管理员"
	}
	email, _ := userEmail(l.ctx, l.svcCtx, u.Id)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("👤 用户详情\n━━━━━━━━━━━━━━━━━━\nID：%d\n邮箱：%s\n状态：%s\n角色：%s\n余额：¥%.2f\n注册：%s\n推荐码：%s\n",
		u.Id, email, enable, adminFlag,
		float64(u.Balance)/100,
		u.CreatedAt.Format("2006-01-02"),
		u.ReferCode,
	))

	auths, _ := l.svcCtx.Store.UserAuth().FindUserAuthMethods(l.ctx, u.Id)
	if len(auths) > 0 {
		sb.WriteString("\n绑定方式：\n")
		for _, a := range auths {
			sb.WriteString(fmt.Sprintf("  • %s\n", a.AuthType))
		}
	}

	if len(subs) > 0 {
		sb.WriteString("\n─── 当前订阅 ───\n")
		for _, s := range subs {
			used := s.Download + s.Upload
			usedGB := float64(used) / (1024 * 1024 * 1024)
			trafficGB := float64(s.Traffic) / (1024 * 1024 * 1024)
			daysLeft := int(time.Until(s.ExpireTime).Hours() / 24)
			expiryWarn := ""
			if daysLeft <= 3 {
				expiryWarn = " ⚠️即将过期"
			}
			name := ""
			if s.Subscribe != nil {
				name = s.Subscribe.Name
			}
			sb.WriteString(fmt.Sprintf("📦 %s (ID:%d)\n   流量：%.1f/%.1fGB  到期：%s (剩%d天)%s\n\n",
				name, s.Id, usedGB, trafficGB,
				s.ExpireTime.Format("2006-01-02"), daysLeft, expiryWarn,
			))
		}
	}
	sb.WriteString("━━━━━━━━━━━━━━━━━━\n📌 快捷操作：\n")
	for _, s := range subs {
		sb.WriteString(fmt.Sprintf("  /reset_%d 重置  /toggle_%d 启停\n", s.Id, s.Id))
	}
	banOp := "禁用"
	if u.Enable != nil && !*u.Enable {
		banOp = "启用"
	}
	sb.WriteString(fmt.Sprintf("\n  /user_sub_%d  /user_log_%d  /ban_%d %s",
		u.Id, u.Id, u.Id, banOp,
	))

	_ = l.sendMessage(l.svcCtx.TelegramBot, sb.String(), msg.Chat.ID)
}

func (l *TelegramLogic) userSubs(msg *tgbotapi.Message, adminUser *user.User, input string) {
	u, ok := l.lookupUser(msg, input)
	if !ok {
		return
	}
	subs, _ := l.svcCtx.Store.UserSubscription().QueryUserSubscribe(l.ctx, u.Id)
	if len(subs) == 0 {
		_ = l.sendMessage(l.svcCtx.TelegramBot, "用户无订阅。", msg.Chat.ID)
		return
	}
	var sb strings.Builder
	email, _ := userEmail(l.ctx, l.svcCtx, u.Id)
	sb.WriteString(fmt.Sprintf("📦 用户 %s 订阅列表 (%d)\n", email, len(subs)))
	for i, s := range subs {
		status := subStatusName(s.Status)
		name := ""
		if s.Subscribe != nil {
			name = s.Subscribe.Name
		}
		sb.WriteString(fmt.Sprintf("\n%d. %s (ID:%d)\n   %s\n   到期：%s\n",
			i+1, name, s.Id, status,
			s.ExpireTime.Format("2006-01-02 15:04"),
		))
	}
	_ = l.sendMessage(l.svcCtx.TelegramBot, sb.String(), msg.Chat.ID)
}

func (l *TelegramLogic) userLogs(msg *tgbotapi.Message, adminUser *user.User, input string) {
	u, ok := l.lookupUser(msg, input)
	if !ok {
		return
	}
	email, _ := userEmail(l.ctx, l.svcCtx, u.Id)
	logs, _, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, &log.FilterParams{
		Page:     1,
		Size:     10,
		Type:     log.TypeLogin.Uint8(),
		ObjectID: u.Id,
	})
	if err != nil {
		l.Errorw("user logs failed", logger.Field("error", err.Error()))
		_ = l.sendMessage(l.svcCtx.TelegramBot, "查询日志失败。", msg.Chat.ID)
		return
	}
	if len(logs) == 0 {
		_ = l.sendMessage(l.svcCtx.TelegramBot, fmt.Sprintf("📜 %s 无登录日志。", email), msg.Chat.ID)
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📜 %s 最近登录 (最多10)\n", email))
	for _, entry := range logs {
		var entryLog log.Login
		if err := entryLog.Unmarshal([]byte(entry.Content)); err != nil {
			continue
		}
		marker := "❌"
		if entryLog.Success {
			marker = "✅"
		}
		sb.WriteString(fmt.Sprintf("%s %s  %s  %s\n",
			marker, entry.CreatedAt.Format("01-02 15:04"),
			entryLog.LoginIP, entryLog.Method,
		))
	}
	_ = l.sendMessage(l.svcCtx.TelegramBot, sb.String(), msg.Chat.ID)
}

// ─────────────────────────────────────
// Mutations (with confirm)
// ─────────────────────────────────────

func (l *TelegramLogic) confirmResetTraffic(msg *tgbotapi.Message, adminUser *user.User, idStr string) {
	subID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		_ = l.sendMessage(l.svcCtx.TelegramBot, "订阅ID格式错误。", msg.Chat.ID)
		return
	}
	sub, err := l.svcCtx.Store.UserSubscription().FindOneSubscribe(l.ctx, subID)
	if err != nil {
		_ = l.sendMessage(l.svcCtx.TelegramBot, "订阅不存在。", msg.Chat.ID)
		return
	}
	actionID := l.saveAction("reset", adminUser.Id, strconv.FormatInt(subID, 10), sub.Token)
	usedStr := trafficGB(sub.Download + sub.Upload)
	_ = l.sendMessage(l.svcCtx.TelegramBot,
		fmt.Sprintf("确认重置 订阅(ID:%d)流量？\n  已用：%s\n\n/confirm_%s 确认\n/cancel_%s 取消",
			subID, usedStr, actionID, actionID),
		msg.Chat.ID)
}

func (l *TelegramLogic) confirmToggleSub(msg *tgbotapi.Message, adminUser *user.User, idStr string) {
	subID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		_ = l.sendMessage(l.svcCtx.TelegramBot, "订阅ID格式错误。", msg.Chat.ID)
		return
	}
	userSub, err := l.svcCtx.Store.UserSubscription().FindOneSubscribe(l.ctx, subID)
	if err != nil {
		_ = l.sendMessage(l.svcCtx.TelegramBot, "订阅不存在。", msg.Chat.ID)
		return
	}
	opLabel := "暂停"
	if userSub.Status == 5 {
		opLabel = "启用"
	}
	actionID := l.saveAction("toggle", adminUser.Id, strconv.FormatInt(subID, 10), "")
	_ = l.sendMessage(l.svcCtx.TelegramBot,
		fmt.Sprintf("确认%s订阅 (ID:%d) ？\n/confirm_%s 确认\n/cancel_%s 取消",
			opLabel, subID, actionID, actionID),
		msg.Chat.ID)
}

func (l *TelegramLogic) confirmBanUser(msg *tgbotapi.Message, adminUser *user.User, input string) {
	u, ok := l.lookupUser(msg, input)
	if !ok {
		return
	}
	if u.Id == adminUser.Id {
		_ = l.sendMessage(l.svcCtx.TelegramBot, "无法对自己的账号执行此操作。", msg.Chat.ID)
		return
	}
	opLabel := "禁用"
	if u.Enable != nil && !*u.Enable {
		opLabel = "启用"
	}
	actionID := l.saveAction("ban", adminUser.Id, strconv.FormatInt(u.Id, 10), "")
	email, _ := userEmail(l.ctx, l.svcCtx, u.Id)
	_ = l.sendMessage(l.svcCtx.TelegramBot,
		fmt.Sprintf("确认%s用户 %s (ID:%d) ？\n/confirm_%s 确认\n/cancel_%s 取消",
			opLabel, email, u.Id, actionID, actionID),
		msg.Chat.ID)
}

func (l *TelegramLogic) confirmAction(msg *tgbotapi.Message, adminUser *user.User, actionID string) {
	act, ok := l.loadAction(actionID, adminUser.Id)
	if !ok {
		_ = l.sendMessage(l.svcCtx.TelegramBot, "操作已过期或无效。", msg.Chat.ID)
		return
	}
	switch act.Cmd {
	case "close":
		id, _ := strconv.ParseInt(act.Target, 10, 64)
		if err := l.svcCtx.Store.Ticket().UpdateTicketStatus(l.ctx, id, 0, ticket.Closed); err != nil {
			l.Errorw("close ticket failed", logger.Field("error", err.Error()))
			_ = l.sendMessage(l.svcCtx.TelegramBot, "关闭工单失败。", msg.Chat.ID)
			return
		}
		_ = l.sendMessage(l.svcCtx.TelegramBot, fmt.Sprintf("✅ 工单 #%d 已关闭", id), msg.Chat.ID)
	case "reset":
		id, _ := strconv.ParseInt(act.Target, 10, 64)
		userSub, err := l.svcCtx.Store.UserSubscription().FindOneSubscribe(l.ctx, id)
		if err != nil {
			_ = l.sendMessage(l.svcCtx.TelegramBot, "订阅不存在。", msg.Chat.ID)
			return
		}
		userSub.Download = 0
		userSub.Upload = 0
		if err := l.svcCtx.Store.UserSubscription().UpdateSubscribe(l.ctx, userSub); err != nil {
			l.Errorw("reset traffic failed", logger.Field("error", err.Error()))
			_ = l.sendMessage(l.svcCtx.TelegramBot, "重置流量失败。", msg.Chat.ID)
			return
		}
		_ = l.svcCtx.Store.UserCache().ClearSubscribeCache(l.ctx, userSub)
		_ = l.svcCtx.Store.Subscribe().ClearCache(l.ctx, userSub.SubscribeId)
		_ = l.sendMessage(l.svcCtx.TelegramBot,
			fmt.Sprintf("✅ 订阅 ID:%d 流量已重置", id), msg.Chat.ID)
	case "toggle":
		id, _ := strconv.ParseInt(act.Target, 10, 64)
		userSub, err := l.svcCtx.Store.UserSubscription().FindOneSubscribe(l.ctx, id)
		if err != nil {
			_ = l.sendMessage(l.svcCtx.TelegramBot, "订阅不存在。", msg.Chat.ID)
			return
		}
		var newStatus uint8 = 1
		opLabel := "已启用"
		if userSub.Status == 1 {
			newStatus = 5
			opLabel = "已暂停"
		}
		userSub.Status = newStatus
		if err := l.svcCtx.Store.UserSubscription().UpdateSubscribe(l.ctx, userSub); err != nil {
			l.Errorw("toggle sub failed", logger.Field("error", err.Error()))
			_ = l.sendMessage(l.svcCtx.TelegramBot, "操作失败。", msg.Chat.ID)
			return
		}
		_ = l.svcCtx.Store.UserCache().ClearSubscribeCache(l.ctx, userSub)
		_ = l.svcCtx.Store.Subscribe().ClearCache(l.ctx, userSub.SubscribeId)
		_ = l.sendMessage(l.svcCtx.TelegramBot,
			fmt.Sprintf("✅ 订阅 ID:%d %s", id, opLabel), msg.Chat.ID)
	case "ban":
		id, _ := strconv.ParseInt(act.Target, 10, 64)
		u, err := l.svcCtx.Store.User().FindOne(l.ctx, id)
		if err != nil {
			_ = l.sendMessage(l.svcCtx.TelegramBot, "用户不存在。", msg.Chat.ID)
			return
		}
		enable := false
		opLabel := "已禁用"
		if u.Enable != nil && !*u.Enable {
			enable = true
			opLabel = "已启用"
		}
		u.Enable = &enable
		if err := l.svcCtx.Store.User().Update(l.ctx, u); err != nil {
			l.Errorw("ban user failed", logger.Field("error", err.Error()))
			_ = l.sendMessage(l.svcCtx.TelegramBot, "操作失败。", msg.Chat.ID)
			return
		}
		_ = l.sendMessage(l.svcCtx.TelegramBot,
			fmt.Sprintf("✅ 用户 (ID:%d) %s", u.Id, opLabel), msg.Chat.ID)
	default:
		_ = l.sendMessage(l.svcCtx.TelegramBot, "未知操作。", msg.Chat.ID)
	}
	l.svcCtx.Redis.Del(l.ctx, tgActionPrefix+actionID)
}

// ─────────────────────────────────────
// Action token (Redis)
// ─────────────────────────────────────

func (l *TelegramLogic) saveAction(cmd string, adminID int64, target, extra string) string {
	actionID := random.KeyNew(8, 1)
	data, _ := json.Marshal(&tgAction{Cmd: cmd, AdminID: adminID, Target: target, Extra: extra})
	_ = l.svcCtx.Redis.Set(l.ctx, tgActionPrefix+actionID, string(data), tgActionTTL).Err()
	return actionID
}

func (l *TelegramLogic) loadAction(actionID string, adminID int64) (tgAction, bool) {
	val, err := l.svcCtx.Redis.Get(l.ctx, tgActionPrefix+actionID).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			l.Errorw("load action failed", logger.Field("error", err.Error()))
		}
		return tgAction{}, false
	}
	var act tgAction
	if err := json.Unmarshal([]byte(val), &act); err != nil {
		return tgAction{}, false
	}
	if act.AdminID != adminID {
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
	case 0:
		return "⏳ 待激活"
	case 1:
		return "✅ 活跃"
	case 2:
		return "🟢 已完成"
	case 3:
		return "⚪ 已过期"
	case 4:
		return "💸 已扣量"
	case 5:
		return "🛑 已暂停"
	}
	return fmt.Sprintf("状态%d", s)
}

func trafficGB(bytes int64) string {
	gb := float64(bytes) / (1024 * 1024 * 1024)
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
