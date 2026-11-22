package portal

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/backend/internal/module/billing"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
)

// statusService records the status request it served and answers it with a
// session token when it has one.
type statusService struct {
	billing.Service
	got   *dto.QueryPurchaseOrderRequest
	token string
}

func (s *statusService) QueryPurchaseOrder(_ context.Context, req *dto.QueryPurchaseOrderRequest) (*dto.QueryPurchaseOrderResponse, error) {
	s.got = req
	return &dto.QueryPurchaseOrderResponse{OrderNo: req.OrderNo, Status: 2, Token: s.token}, nil
}

// serveStatus gets uri, with the capability header when header is set.
func serveStatus(svc billing.Service, uri, header string) *app.RequestContext {
	engine := server.Default()
	ctx := engine.NewContext()
	ctx.Request.Header.SetMethod(http.MethodGet)
	ctx.Request.SetRequestURI(uri)
	if header != "" {
		ctx.Request.Header.Set(checkoutTokenHeader, header)
	}
	QueryPurchaseOrderHandler(svc)(context.Background(), ctx)
	return ctx
}

// The capability travels in a header, out of access logs and browser
// history; the query parameter keeps working and loses to the header. An
// answer carrying a session token must not be cached.
func TestQueryPurchaseOrderHandlerReadsTheCapabilityHeader(t *testing.T) {
	svc := &statusService{token: "session-token"}
	const status = "/v1/public/portal/order/status?order_no=order-1"

	ctx := serveStatus(svc, status, "header-capability")
	if svc.got.OrderNo != "order-1" || svc.got.CheckoutToken != "header-capability" {
		t.Fatalf("request = %+v, want the header capability", svc.got)
	}
	if ctx.Response.Header.Get("Cache-Control") != "no-store" || !strings.Contains(string(ctx.Response.Body()), "session-token") {
		t.Fatalf("answer with a token: Cache-Control %q, body %s", ctx.Response.Header.Get("Cache-Control"), ctx.Response.Body())
	}
	if serveStatus(svc, status+"&checkout_token=query-capability", ""); svc.got.CheckoutToken != "query-capability" {
		t.Fatalf("request = %+v, want the query capability", svc.got)
	}
	if serveStatus(svc, status+"&checkout_token=query-capability", "header-capability"); svc.got.CheckoutToken != "header-capability" {
		t.Fatalf("request = %+v, want the header to win", svc.got)
	}

	svc.token = ""
	if ctx := serveStatus(svc, status, "header-capability"); ctx.Response.Header.Get("Cache-Control") != "" {
		t.Fatalf("answer without a token: Cache-Control %q, want none", ctx.Response.Header.Get("Cache-Control"))
	}
}
