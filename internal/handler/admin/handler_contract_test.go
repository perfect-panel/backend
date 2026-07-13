package admin_test

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/handler/admin/ads"
	"github.com/perfect-panel/server/internal/handler/admin/announcement"
	"github.com/perfect-panel/server/internal/handler/admin/application"
	"github.com/perfect-panel/server/internal/handler/admin/authMethod"
	"github.com/perfect-panel/server/internal/handler/admin/console"
	"github.com/perfect-panel/server/internal/handler/admin/coupon"
	"github.com/perfect-panel/server/internal/handler/admin/document"
	adminlog "github.com/perfect-panel/server/internal/handler/admin/log"
	"github.com/perfect-panel/server/internal/svc"
)

func TestHandlerFactories_returnNativeHertzHandlers(t *testing.T) {
	// Given all owned admin handler factories
	// When their factory signatures are checked at compile time
	// Then each factory returns Hertz's native handler type.
	_ = t
	var _ func(*svc.ServiceContext) app.HandlerFunc = ads.CreateAdsHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = ads.DeleteAdsHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = ads.GetAdsDetailHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = ads.GetAdsListHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = ads.UpdateAdsHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = announcement.CreateAnnouncementHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = announcement.DeleteAnnouncementHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = announcement.GetAnnouncementHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = announcement.GetAnnouncementListHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = announcement.UpdateAnnouncementHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = application.CreateSubscribeApplicationHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = application.DeleteSubscribeApplicationHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = application.GetSubscribeApplicationListHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = application.PreviewSubscribeTemplateHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = application.UpdateSubscribeApplicationHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = authMethod.GetAuthMethodConfigHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = authMethod.GetAuthMethodListHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = authMethod.GetEmailPlatformHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = authMethod.GetSmsPlatformHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = authMethod.TestEmailSendHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = authMethod.TestSmsSendHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = authMethod.UpdateAuthMethodConfigHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = console.QueryRevenueStatisticsHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = console.QueryServerTotalDataHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = console.QueryTicketWaitReplyHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = console.QueryUserStatisticsHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = coupon.BatchDeleteCouponHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = coupon.CreateCouponHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = coupon.DeleteCouponHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = coupon.GetCouponListHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = coupon.UpdateCouponHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = document.BatchDeleteDocumentHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = document.CreateDocumentHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = document.DeleteDocumentHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = document.GetDocumentDetailHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = document.GetDocumentListHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = document.UpdateDocumentHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = adminlog.FilterBalanceLogHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = adminlog.FilterCommissionLogHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = adminlog.FilterEmailLogHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = adminlog.FilterGiftLogHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = adminlog.FilterLoginLogHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = adminlog.FilterMobileLogHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = adminlog.FilterRegisterLogHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = adminlog.FilterResetSubscribeLogHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = adminlog.FilterServerTrafficLogHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = adminlog.FilterSubscribeLogHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = adminlog.FilterTrafficLogDetailsHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = adminlog.FilterUserSubscribeTrafficLogHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = adminlog.GetLogSettingHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = adminlog.GetMessageLogListHandler
	var _ func(*svc.ServiceContext) app.HandlerFunc = adminlog.UpdateLogSettingHandler
}
