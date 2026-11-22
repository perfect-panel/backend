package subscribe

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
)

// handlerFactory compiles only for a handler factory that takes S and returns
// Hertz's native handler type.
func handlerFactory[S any](func(S) app.HandlerFunc) {}

func TestHandlerFactories_return_native_hertz_handlers(t *testing.T) {
	handlerFactory[subscription.Service](BatchDeleteSubscribeGroupHandler)
	handlerFactory[subscription.Service](BatchDeleteSubscribeHandler)
	handlerFactory[subscription.Service](CreateSubscribeGroupHandler)
	handlerFactory[subscription.Service](CreateSubscribeHandler)
	handlerFactory[subscription.Service](DeleteSubscribeGroupHandler)
	handlerFactory[subscription.Service](DeleteSubscribeHandler)
	handlerFactory[subscription.Service](GetSubscribeDetailsHandler)
	handlerFactory[subscription.Service](GetSubscribeGroupListHandler)
	handlerFactory[subscription.Service](GetSubscribeListHandler)
	handlerFactory[subscription.Service](ResetAllSubscribeTokenHandler)
	handlerFactory[subscription.Service](SubscribeSortHandler)
	handlerFactory[subscription.Service](UpdateSubscribeGroupHandler)
	handlerFactory[subscription.Service](UpdateSubscribeHandler)
}
