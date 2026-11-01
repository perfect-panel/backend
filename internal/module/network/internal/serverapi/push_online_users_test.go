package serverapi

import (
	"context"
	"fmt"
	"strings"
	"testing"

	dto "github.com/perfect-panel/server/internal/module/network/contract"
	"github.com/perfect-panel/server/internal/module/network/entity/node"
)

// onlineRecorder records what PushOnlineUsers stores.
type onlineRecorder struct {
	serverID int64
	protocol string
	server   node.OnlineUserSubscribe
	global   node.OnlineUserSubscribe
	writes   int
}

var _ OnlineRecorder = (*onlineRecorder)(nil)

func (r *onlineRecorder) UpdateOnlineUserSubscribe(_ context.Context, serverID int64, protocol string, subscribe node.OnlineUserSubscribe) error {
	r.serverID, r.protocol, r.server = serverID, protocol, subscribe
	r.writes++
	return nil
}

func (r *onlineRecorder) UpdateOnlineUserSubscribeGlobal(_ context.Context, subscribe node.OnlineUserSubscribe) error {
	r.global = subscribe
	r.writes++
	return nil
}

func onlineRequest(users ...dto.OnlineUser) *dto.OnlineUsersRequest {
	return &dto.OnlineUsersRequest{ServerCommon: dto.ServerCommon{ServerId: 4, Protocol: "vless"}, Users: users}
}

// Only the subscriptions the server serves (its user list: 21, not 22 whose
// owner is disabled, nor 99) are recorded, with their addresses in
// canonical form.
func TestPushOnlineUsersRecordsOnlyServedSubscriptions(t *testing.T) {
	deps, _, _ := newScopeDeps(t)
	recorder := &onlineRecorder{}
	deps.Online = recorder
	err := NewService(deps).PushOnlineUsers(context.Background(), onlineRequest(
		dto.OnlineUser{SID: 21, IP: "1.2.3.4"},
		dto.OnlineUser{SID: 21, IP: "::ffff:5.6.7.8"},
		dto.OnlineUser{SID: 21, IP: "2001:DB8::1"},
		dto.OnlineUser{SID: 22, IP: "9.9.9.9"},
		dto.OnlineUser{SID: 99, IP: "8.8.8.8"},
	))
	if err != nil {
		t.Fatalf("PushOnlineUsers: %v", err)
	}
	if recorder.serverID != 4 || recorder.protocol != "vless" || recorder.writes != 2 {
		t.Fatalf("recorded server %d/%s in %d writes", recorder.serverID, recorder.protocol, recorder.writes)
	}
	want := node.OnlineUserSubscribe{21: {"1.2.3.4", "5.6.7.8", "2001:db8::1"}}
	for name, got := range map[string]node.OnlineUserSubscribe{"server": recorder.server, "global": recorder.global} {
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s online users = %v, want %v", name, got, want)
		}
	}
}

// A report with an address that is not an IP, or over the size bound, is
// refused before anything is recorded.
func TestPushOnlineUsersRefusesMalformedAndOversizedReports(t *testing.T) {
	deps, _, _ := newScopeDeps(t)
	recorder := &onlineRecorder{}
	deps.Online = recorder
	service := NewService(deps)

	for name, req := range map[string]*dto.OnlineUsersRequest{
		"not an ip": onlineRequest(dto.OnlineUser{SID: 21, IP: "1.2.3.4"}, dto.OnlineUser{SID: 21, IP: "not-an-ip"}),
		"host":      onlineRequest(dto.OnlineUser{SID: 21, IP: "example.com"}),
		"too long":  onlineRequest(dto.OnlineUser{SID: 21, IP: strings.Repeat("1", maxIPTextLength+1)}),
		"empty ip":  onlineRequest(dto.OnlineUser{SID: 21, IP: ""}),
		"no sid":    onlineRequest(dto.OnlineUser{SID: 0, IP: "1.2.3.4"}),
		"no users":  onlineRequest(),
		"no server": {ServerCommon: dto.ServerCommon{Protocol: "vless"}, Users: []dto.OnlineUser{{SID: 21, IP: "1.2.3.4"}}},
		"oversized": oversizedOnlineRequest(),
	} {
		if err := service.PushOnlineUsers(context.Background(), req); err == nil {
			t.Fatalf("%s: report accepted", name)
		}
	}
	if recorder.writes != 0 {
		t.Fatalf("a refused report recorded %d writes", recorder.writes)
	}
}

func oversizedOnlineRequest() *dto.OnlineUsersRequest {
	users := make([]dto.OnlineUser, maxOnlineUsersPerReport+1)
	for i := range users {
		users[i] = dto.OnlineUser{SID: 21, IP: "1.2.3.4"}
	}
	return onlineRequest(users...)
}

// The request-wide bound remains, but legitimate device limits above 64
// must not silently lose heartbeat renewals for the remaining addresses.
func TestPushOnlineUsersKeepsAllUniqueAddressesWithinReportBound(t *testing.T) {
	deps, _, _ := newScopeDeps(t)
	recorder := &onlineRecorder{}
	deps.Online = recorder
	users := make([]dto.OnlineUser, 0, 74)
	for i := range 74 {
		users = append(users, dto.OnlineUser{SID: 21, IP: fmt.Sprintf("10.0.%d.%d", i/256, i%256)})
	}
	if err := NewService(deps).PushOnlineUsers(context.Background(), onlineRequest(users...)); err != nil {
		t.Fatalf("PushOnlineUsers: %v", err)
	}
	if got := len(recorder.server[21]); got != 74 {
		t.Fatalf("recorded %d addresses for one subscription, want 74", got)
	}
}
