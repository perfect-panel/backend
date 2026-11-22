package arch_test

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/billing"
	"github.com/perfect-panel/backend/internal/module/billing/transport/http/admin/coupon"
	"github.com/perfect-panel/backend/internal/module/identity"
	"github.com/perfect-panel/backend/internal/module/identity/transport/http/admin/authmethod"
	"github.com/perfect-panel/backend/internal/module/platform/transport/http/admin/console"
	adminlog "github.com/perfect-panel/backend/internal/module/platform/transport/http/admin/log"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/module/subscription/transport/http/admin/application"
	"github.com/perfect-panel/backend/internal/module/support"
	"github.com/perfect-panel/backend/internal/module/support/transport/http/admin/ads"
	"github.com/perfect-panel/backend/internal/module/support/transport/http/admin/announcement"
	"github.com/perfect-panel/backend/internal/module/support/transport/http/admin/document"
)

// handlerFactory compiles only when its argument takes exactly the facade S
// and returns Hertz's native handler type. The explicit type argument makes
// the call the check, so it has no effect at run time.
func handlerFactory[S any](func(S) app.HandlerFunc) {}

func TestHandlerFactories_returnNativeHertzHandlers(t *testing.T) {
	// Given all owned admin handler factories
	// When their factory signatures are checked at compile time
	// Then each factory returns Hertz's native handler type.
	_ = t
	handlerFactory[support.Service](ads.CreateAdsHandler)
	handlerFactory[support.Service](ads.DeleteAdsHandler)
	handlerFactory[support.Service](ads.GetAdsDetailHandler)
	handlerFactory[support.Service](ads.GetAdsListHandler)
	handlerFactory[support.Service](ads.UpdateAdsHandler)
	handlerFactory[support.Service](announcement.CreateAnnouncementHandler)
	handlerFactory[support.Service](announcement.DeleteAnnouncementHandler)
	handlerFactory[support.Service](announcement.GetAnnouncementHandler)
	handlerFactory[support.Service](announcement.GetAnnouncementListHandler)
	handlerFactory[support.Service](announcement.UpdateAnnouncementHandler)
	handlerFactory[subscription.Service](application.CreateSubscribeApplicationHandler)
	handlerFactory[subscription.Service](application.DeleteSubscribeApplicationHandler)
	handlerFactory[subscription.Service](application.GetSubscribeApplicationListHandler)
	handlerFactory[subscription.Service](application.PreviewSubscribeTemplateHandler)
	handlerFactory[subscription.Service](application.UpdateSubscribeApplicationHandler)
	handlerFactory[identity.Service](authmethod.GetAuthMethodConfigHandler)
	handlerFactory[identity.Service](authmethod.GetAuthMethodListHandler)
	handlerFactory[identity.Service](authmethod.GetEmailPlatformHandler)
	handlerFactory[identity.Service](authmethod.GetSmsPlatformHandler)
	handlerFactory[identity.Service](authmethod.TestEmailSendHandler)
	handlerFactory[identity.Service](authmethod.TestSmsSendHandler)
	handlerFactory[identity.Service](authmethod.UpdateAuthMethodConfigHandler)
	handlerFactory[console.Dashboard](console.QueryRevenueStatisticsHandler)
	handlerFactory[console.Dashboard](console.QueryServerTotalDataHandler)
	handlerFactory[console.Dashboard](console.QueryTicketWaitReplyHandler)
	handlerFactory[console.Dashboard](console.QueryUserStatisticsHandler)
	handlerFactory[billing.Service](coupon.BatchDeleteCouponHandler)
	handlerFactory[billing.Service](coupon.CreateCouponHandler)
	handlerFactory[billing.Service](coupon.DeleteCouponHandler)
	handlerFactory[billing.Service](coupon.GetCouponListHandler)
	handlerFactory[billing.Service](coupon.UpdateCouponHandler)
	handlerFactory[support.Service](document.BatchDeleteDocumentHandler)
	handlerFactory[support.Service](document.CreateDocumentHandler)
	handlerFactory[support.Service](document.DeleteDocumentHandler)
	handlerFactory[support.Service](document.GetDocumentDetailHandler)
	handlerFactory[support.Service](document.GetDocumentListHandler)
	handlerFactory[support.Service](document.UpdateDocumentHandler)
	handlerFactory[adminlog.Logs](adminlog.FilterBalanceLogHandler)
	handlerFactory[adminlog.Logs](adminlog.FilterCommissionLogHandler)
	handlerFactory[adminlog.Logs](adminlog.FilterEmailLogHandler)
	handlerFactory[adminlog.Logs](adminlog.FilterGiftLogHandler)
	handlerFactory[adminlog.Logs](adminlog.FilterLoginLogHandler)
	handlerFactory[adminlog.Logs](adminlog.FilterMobileLogHandler)
	handlerFactory[adminlog.Logs](adminlog.FilterOrderLogHandler)
	handlerFactory[adminlog.Logs](adminlog.FilterRegisterLogHandler)
	handlerFactory[adminlog.Logs](adminlog.FilterResetSubscribeLogHandler)
	handlerFactory[adminlog.Logs](adminlog.FilterServerTrafficLogHandler)
	handlerFactory[adminlog.Logs](adminlog.FilterSubscribeLogHandler)
	handlerFactory[adminlog.Logs](adminlog.FilterTrafficLogDetailsHandler)
	handlerFactory[adminlog.Logs](adminlog.FilterUserSubscribeTrafficLogHandler)
	handlerFactory[adminlog.Logs](adminlog.GetLogSettingHandler)
	handlerFactory[adminlog.Logs](adminlog.GetMessageLogListHandler)
	handlerFactory[adminlog.Logs](adminlog.UpdateLogSettingHandler)
}
