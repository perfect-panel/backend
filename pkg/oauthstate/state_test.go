package oauthstate

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestConsume(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	ctx := context.Background()
	if err := client.Set(ctx, "google:state", "https://example.com/callback", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}

	value, err := Consume(ctx, client, "google:state")
	if err != nil {
		t.Fatalf("consume state: %v", err)
	}
	if value != "https://example.com/callback" {
		t.Fatalf("value = %q", value)
	}
	if _, err := Consume(ctx, client, "google:state"); err == nil {
		t.Fatal("expected consumed state to be missing")
	}
}
