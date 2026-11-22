package serverapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"uuid"

	"github.com/alicebob/miniredis/v2"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

func TestPlaceholderServerUserUsesNewUUIDV7(t *testing.T) {
	seen := make(map[uuid.UUID]bool)
	for range 32 {
		user := placeholderServerUser()
		id, err := uuid.Parse(user.UUID)
		if err != nil {
			t.Fatal(err)
		}
		if user.Id != 1 || id[6]>>4 != 7 || id[8]>>6 != 2 {
			t.Fatalf("invalid V7 placeholder: %+v", user)
		}
		if seen[id] {
			t.Fatal("placeholder generation reused a UUID")
		}
		seen[id] = true
	}
}

func TestCachedPlaceholderKeepsUUIDAndETag(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	placeholder := placeholderServerUser()
	payload, err := json.Marshal(dto.GetServerUserListResponse{Users: []dto.ServerUser{placeholder}})
	if err != nil {
		t.Fatal(err)
	}
	req := &dto.GetServerUserListRequest{ServerId: 1, Protocol: "vless"}
	if err := server.Set(fmt.Sprintf("%s%d:%s", node.ServerUserListCacheKey, req.ServerId, req.Protocol), string(payload)); err != nil {
		t.Fatal(err)
	}
	// No repositories are provided: a cache hit must not rebuild the list.
	service := NewService(Deps{Redis: client})
	etag := httpx.GenerateETag(payload)
	for range 2 {
		resp, meta, err := service.GetServerUserList(context.Background(), req, RequestMeta{})
		if err != nil || len(resp.Users) != 1 || resp.Users[0].UUID != placeholder.UUID {
			t.Fatalf("cached user list changed: %+v, %v", resp, err)
		}
		if meta.Headers["ETag"] != etag {
			t.Fatalf("cached user list ETag = %q, want %q", meta.Headers["ETag"], etag)
		}
	}
	if resp, _, err := service.GetServerUserList(context.Background(), req, RequestMeta{IfNoneMatch: etag}); resp != nil || !errors.Is(err, xerr.ErrNotModified) {
		t.Fatalf("cached ETag no longer returns not-modified: %+v, %v", resp, err)
	}
}
