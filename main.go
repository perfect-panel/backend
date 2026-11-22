package main

import (
	// The Docker image is built from scratch and ships the Asia/Shanghai
	// zone file only, so the binary carries the zone database itself for
	// every other AppLocation. The time package consults the embedded copy
	// after the system directories, so a system database still wins where
	// one exists.
	_ "time/tzdata"

	"github.com/perfect-panel/backend/cmd"
)

// @title PPanel API
// @version 1.0
// @description HTTP API for PPanel's Hertz server. Application errors are returned in the response code and message fields.
// @accept json
// @produce json
// @schemes http https
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @securityDefinitions.apikey NodeSecret
// @in query
// @name secret_key
// @securityDefinitions.apikey TelegramSecret
// @in header
// @name X-Telegram-Bot-Api-Secret-Token
func main() {
	cmd.Execute()
}
