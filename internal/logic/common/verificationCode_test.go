package common

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestVerificationCodeLifecycle(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	ctx := context.Background()
	key := "auth:verify:email:register:alice@example.com"

	if err := SaveVerificationCode(ctx, client, key, "123456", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := ValidateVerificationCode(ctx, client, key, "000000", false); !errors.Is(err, ErrVerificationCodeInvalid) {
		t.Fatalf("wrong code error = %v", err)
	}
	if err := ValidateVerificationCode(ctx, client, key, "123456", false); err != nil {
		t.Fatalf("pre-check failed: %v", err)
	}
	if err := ValidateVerificationCode(ctx, client, key, "123456", true); err != nil {
		t.Fatalf("consume failed: %v", err)
	}
	if err := ValidateVerificationCode(ctx, client, key, "123456", true); !errors.Is(err, ErrVerificationCodeInvalid) {
		t.Fatalf("consumed code was reusable: %v", err)
	}
}

func TestVerificationCodeAttemptLimit(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	ctx := context.Background()
	key := "auth:verify:email:register:bob@example.com"
	if err := SaveVerificationCode(ctx, client, key, "123456", time.Minute); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= maxVerificationCodeAttempts; i++ {
		err := ValidateVerificationCode(ctx, client, key, "000000", false)
		if i == maxVerificationCodeAttempts {
			if !errors.Is(err, ErrVerificationAttemptsExceeded) {
				t.Fatalf("attempt %d error = %v", i, err)
			}
		} else if !errors.Is(err, ErrVerificationCodeInvalid) {
			t.Fatalf("attempt %d error = %v", i, err)
		}
	}
	if err := ValidateVerificationCode(ctx, client, key, "123456", true); !errors.Is(err, ErrVerificationCodeInvalid) {
		t.Fatalf("code survived attempt limit: %v", err)
	}
}

func TestVerificationCodeConsumesLegacyJSONPayload(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	ctx := context.Background()
	key := "auth:verify:email:register:legacy@example.com"
	if err := client.Set(ctx, key, `{"code":"123456","lastAt":1}`, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateVerificationCode(ctx, client, key, "123456", true); err != nil {
		t.Fatalf("legacy code failed: %v", err)
	}
	if exists := client.Exists(ctx, key).Val(); exists != 0 {
		t.Fatal("legacy code was not consumed")
	}
}

func TestSaveVerificationCodeReplacesLegacyJSONPayload(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	ctx := context.Background()
	key := "auth:verify:email:register:legacy-resend@example.com"
	if err := client.Set(ctx, key, `{"code":"111111","lastAt":1}`, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if err := SaveVerificationCode(ctx, client, key, "222222", time.Minute); err != nil {
		t.Fatalf("replace legacy code: %v", err)
	}
	if err := ValidateVerificationCode(ctx, client, key, "222222", true); err != nil {
		t.Fatalf("replacement code failed: %v", err)
	}
}
