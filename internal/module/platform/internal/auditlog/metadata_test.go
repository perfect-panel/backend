package auditlog

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/internal/repo"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var testRequest = requestmeta.Metadata{
	ClientIP:  "203.0.113.7",
	UserAgent: "RiskClient/1.0",
	ActorID:   9,
	IPMetadata: requestmeta.IPMetadata{
		IPCountryCode: "SG", IPCountry: "Singapore", IPRegion: "Central", IPCity: "Singapore",
		IPASN: 13335, IPASOrganization: "Cloudflare",
	},
}

// assertRequestMetadata checks that item, a log DTO, carries testRequest in
// every request field it has; hasClientIP says whether it has ClientIP.
func assertRequestMetadata(t *testing.T, name string, item any, hasClientIP bool) {
	t.Helper()
	value := reflect.ValueOf(item)
	want := reflect.ValueOf(testRequest)
	for _, field := range requestMetadataFields {
		got := value.FieldByName(field)
		if !got.IsValid() {
			if field == "ClientIP" && !hasClientIP {
				continue
			}
			t.Fatalf("%s: no %s field", name, field)
		}
		if got.Interface() != want.FieldByName(field).Interface() {
			t.Fatalf("%s: %s = %v, want %v", name, field, got.Interface(), want.FieldByName(field).Interface())
		}
	}
}

func TestWithRequestMetadataFillsEveryRequestField(t *testing.T) {
	balance := withRequestMetadata(&dto.BalanceLog{Amount: 5}, testRequest)
	if balance.Amount != 5 {
		t.Fatal("the helper touched a field outside the request metadata")
	}
	assertRequestMetadata(t, "BalanceLog", balance, true)
	assertRequestMetadata(t, "LoginLog", withRequestMetadata(&dto.LoginLog{}, testRequest), false)
}

type marshaler interface{ Marshal() ([]byte, error) }

// Every audit log view returns the request that caused the entry.
func TestLogFiltersReturnRequestMetadata(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:audit-metadata-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&log.SystemLog{}); err != nil {
		t.Fatal(err)
	}
	ip := testRequest.IPMetadata
	for typ, entry := range map[log.Type]marshaler{
		log.TypeBalance:        &log.Balance{Metadata: testRequest, Type: log.BalanceTypeRecharge, Amount: 100},
		log.TypeCommission:     &log.Commission{Metadata: testRequest, Type: log.CommissionTypePurchase, Amount: 10},
		log.TypeGift:           &log.Gift{Metadata: testRequest, Type: log.GiftTypeIncrease, Amount: 1},
		log.TypeOrderCreated:   &log.OrderCreated{Metadata: testRequest, OrderNo: "N1"},
		log.TypeResetSubscribe: &log.ResetSubscribe{Metadata: testRequest, Type: log.ResetSubscribeTypePaid},
		log.TypeEmailMessage:   &log.Message{Metadata: testRequest, Subject: "verify"},
		log.TypeMobileMessage:  &log.Message{Metadata: testRequest, Subject: "verify"},
		log.TypeLogin:          &log.Login{IPMetadata: ip, LoginIP: testRequest.ClientIP, UserAgent: testRequest.UserAgent, ActorID: testRequest.ActorID, Method: "email", Success: true},
		log.TypeRegister:       &log.Register{IPMetadata: ip, RegisterIP: testRequest.ClientIP, UserAgent: testRequest.UserAgent, ActorID: testRequest.ActorID},
		log.TypeSubscribe:      &log.Subscribe{IPMetadata: ip, ClientIP: testRequest.ClientIP, UserAgent: testRequest.UserAgent, ActorID: testRequest.ActorID, UserSubscribeId: 5},
	} {
		content, err := entry.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&log.SystemLog{Type: typ.Uint8(), ObjectID: 7, Date: time.Now().Format(time.DateOnly), Content: string(content)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(Deps{Logs: repo.NewLogRepo(db)})
	ctx := context.Background()
	page := dto.FilterLogParams{Page: 1, Size: 10}

	first := func(name string, list any, err error) any {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		items := reflect.ValueOf(list)
		if items.Len() != 1 {
			t.Fatalf("%s: %d entries, want 1", name, items.Len())
		}
		return items.Index(0).Interface()
	}
	balance, err := svc.FilterBalanceLog(ctx, &dto.FilterBalanceLogRequest{FilterLogParams: page})
	assertRequestMetadata(t, "balance", first("balance", balance.List, err), true)
	commission, err := svc.FilterCommissionLog(ctx, &dto.FilterCommissionLogRequest{FilterLogParams: page})
	assertRequestMetadata(t, "commission", first("commission", commission.List, err), true)
	gift, err := svc.FilterGiftLog(ctx, &dto.FilterGiftLogRequest{FilterLogParams: page})
	assertRequestMetadata(t, "gift", first("gift", gift.List, err), true)
	order, err := svc.FilterOrderLog(ctx, &dto.FilterOrderLogRequest{FilterLogParams: page})
	assertRequestMetadata(t, "order", first("order", order.List, err), true)
	reset, err := svc.FilterResetSubscribeLog(ctx, &dto.FilterResetSubscribeLogRequest{FilterLogParams: page})
	assertRequestMetadata(t, "reset", first("reset", reset.List, err), true)
	email, err := svc.FilterEmailLog(ctx, &page)
	assertRequestMetadata(t, "email", first("email", email.List, err), true)
	mobile, err := svc.FilterMobileLog(ctx, &page)
	assertRequestMetadata(t, "mobile", first("mobile", mobile.List, err), true)
	messages, err := svc.GetMessageLogList(ctx, &dto.GetMessageLogListRequest{Page: 1, Size: 10, Type: log.TypeEmailMessage.Uint8()})
	assertRequestMetadata(t, "messages", first("messages", messages.List, err), true)
	subscribe, err := svc.FilterSubscribeLog(ctx, &dto.FilterSubscribeLogRequest{FilterLogParams: page})
	assertRequestMetadata(t, "subscribe", first("subscribe", subscribe.List, err), true)
	login, err := svc.FilterLoginLog(ctx, &dto.FilterLoginLogRequest{FilterLogParams: page})
	loginItem := first("login", login.List, err)
	assertRequestMetadata(t, "login", loginItem, false)
	if loginItem.(dto.LoginLog).LoginIP != testRequest.ClientIP {
		t.Fatalf("login IP = %q", loginItem.(dto.LoginLog).LoginIP)
	}
	register, err := svc.FilterRegisterLog(ctx, &dto.FilterRegisterLogRequest{FilterLogParams: page})
	assertRequestMetadata(t, "register", first("register", register.List, err), false)
}
