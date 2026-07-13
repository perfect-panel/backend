package tool

import "github.com/cloudwego/hertz/pkg/app"

var (
	_ app.HandlerFunc = GetVersionHandler(nil)
	_ app.HandlerFunc = RestartSystemHandler(nil)
	_ app.HandlerFunc = GetSystemLogHandler(nil)
	_ app.HandlerFunc = QueryIPLocationHandler(nil)
)
