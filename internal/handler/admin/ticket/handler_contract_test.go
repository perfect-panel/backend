package ticket

import "github.com/cloudwego/hertz/pkg/app"

var (
	_ app.HandlerFunc = GetTicketHandler(nil)
	_ app.HandlerFunc = CreateTicketFollowHandler(nil)
	_ app.HandlerFunc = UpdateTicketStatusHandler(nil)
	_ app.HandlerFunc = GetTicketListHandler(nil)
)
