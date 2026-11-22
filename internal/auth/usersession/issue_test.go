package usersession

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/auth/devicesession"
	"github.com/perfect-panel/backend/internal/auth/token"
	"github.com/redis/go-redis/v9"
)

const testSecret = "session-test-secret"

func newTestClient(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

func TestIssuedSessionValidatesUntilEnded(t *testing.T) {
	server, client := newTestClient(t)
	ctx := context.Background()

	signed, err := Issue(ctx, client, testSecret, 3600, Grant{UserID: 7, LoginType: "email"})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	claims, err := Validate(ctx, client, testSecret, signed)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if claims.UserID != 7 || claims.LoginType != "email" || claims.SessionID == "" || claims.DeviceID != 0 {
		t.Fatalf("claims = %+v", claims)
	}
	if ttl := server.TTL(SessionKey(claims.SessionID)); ttl <= 0 || ttl > time.Hour {
		t.Fatalf("session record TTL = %v, want the token lifetime", ttl)
	}

	if err := End(ctx, client, claims.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(ctx, client, testSecret, signed); !errors.Is(err, ErrEnded) {
		t.Fatalf("ended session: error = %v, want ErrEnded", err)
	}
}

func TestValidateRejectsWhatIsNotALiveSession(t *testing.T) {
	_, client := newTestClient(t)
	ctx := context.Background()

	if _, err := Validate(ctx, client, testSecret, "not-a-token"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("garbage token: error = %v, want ErrInvalidToken", err)
	}
	signed, err := Issue(ctx, client, testSecret, 3600, Grant{UserID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(ctx, client, "another-secret", signed); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("foreign secret: error = %v, want ErrInvalidToken", err)
	}
	// Order event tickets share the secret but carry no session.
	ticket, err := token.NewJwtToken(testSecret, time.Now().Unix(), 3600, token.WithOption("OrderNo", "1"), token.WithOption(UserIDClaim, int64(7)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(ctx, client, testSecret, ticket); !errors.Is(err, ErrNotSession) {
		t.Fatalf("ticket: error = %v, want ErrNotSession", err)
	}

	if err := Revoke(ctx, client, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(ctx, client, testSecret, signed); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked session: error = %v, want ErrRevoked", err)
	}
}

func TestDeviceSessionEndsWithItsDevice(t *testing.T) {
	_, client := newTestClient(t)
	ctx := context.Background()

	signed, err := Issue(ctx, client, testSecret, 3600, Grant{UserID: 7, LoginType: "device", DeviceID: 9})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	claims, err := Validate(ctx, client, testSecret, signed)
	if err != nil || claims.DeviceID != 9 || claims.LoginType != "device" {
		t.Fatalf("Validate() = %+v, %v", claims, err)
	}
	other, err := Issue(ctx, client, testSecret, 3600, Grant{UserID: 7, DeviceID: 10})
	if err != nil {
		t.Fatal(err)
	}

	if err := devicesession.Revoke(ctx, client, 9); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(ctx, client, testSecret, signed); !errors.Is(err, ErrDeviceRevoked) {
		t.Fatalf("revoked device: error = %v, want ErrDeviceRevoked", err)
	}
	if _, err := Validate(ctx, client, testSecret, other); err != nil {
		t.Fatalf("another device's session ended too: %v", err)
	}
}

// A device session issued before sessions were bound to devices carries no
// binding and must be renewed.
func TestValidateRejectsUnboundDeviceSession(t *testing.T) {
	_, client := newTestClient(t)
	ctx := context.Background()
	epoch, err := AcquireEpoch(ctx, client, 7)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := token.NewJwtToken(testSecret, time.Now().Unix(), 3600,
		token.WithOption(UserIDClaim, int64(7)), token.WithOption(SessionIDClaim, "legacy"),
		token.WithOption(LoginTypeClaim, "device"), token.WithOption(EpochClaim, epoch))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, SessionKey("legacy"), 7, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(ctx, client, testSecret, legacy); !errors.Is(err, ErrDeviceSession) {
		t.Fatalf("legacy device session: error = %v, want ErrDeviceSession", err)
	}
}

// A sign-in reads the epoch before it checks the credential. When a
// revocation lands before the session is issued, the session is refused
// rather than carrying the new epoch and surviving the revocation.
func TestIssueRefusesASessionWhoseEpochMovedSinceTheCredentialCheck(t *testing.T) {
	_, client := newTestClient(t)
	ctx := context.Background()
	before, err := AcquireEpoch(ctx, client, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := Revoke(ctx, client, 7); err != nil {
		t.Fatal(err)
	}

	if _, err := Issue(ctx, client, testSecret, 3600, Grant{UserID: 7, Epoch: before}); !errors.Is(err, ErrEpochMoved) {
		t.Fatalf("Issue() with the epoch from before the revocation: error = %v, want ErrEpochMoved", err)
	}

	current, err := AcquireEpoch(ctx, client, 7)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := Issue(ctx, client, testSecret, 3600, Grant{UserID: 7, Epoch: current})
	if err != nil {
		t.Fatalf("Issue() with the current epoch: error = %v", err)
	}
	if _, err := Validate(ctx, client, testSecret, signed); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	// Without a pre-read epoch the session is issued as before.
	if _, err := Issue(ctx, client, testSecret, 3600, Grant{UserID: 7}); err != nil {
		t.Fatalf("Issue() without an epoch: error = %v", err)
	}
}

// A reset revokes and signs in at once: the session it issues carries the
// epoch the revocation set, and another revocation in between refuses it.
func TestRotateReturnsTheEpochNewSessionsCarry(t *testing.T) {
	_, client := newTestClient(t)
	ctx := context.Background()
	earlier, err := Issue(ctx, client, testSecret, 3600, Grant{UserID: 7})
	if err != nil {
		t.Fatal(err)
	}

	epoch, err := Rotate(ctx, client, 7)
	if err != nil || epoch == "" {
		t.Fatalf("Rotate() = %q, %v", epoch, err)
	}
	if stored, _ := client.Get(ctx, Key(7)).Result(); stored != epoch {
		t.Fatalf("stored epoch = %q, want the rotated %q", stored, epoch)
	}
	if _, err := Validate(ctx, client, testSecret, earlier); !errors.Is(err, ErrRevoked) {
		t.Fatalf("session from before the rotation: error = %v, want ErrRevoked", err)
	}
	signed, err := Issue(ctx, client, testSecret, 3600, Grant{UserID: 7, Epoch: epoch})
	if err != nil {
		t.Fatalf("Issue() with the rotated epoch: error = %v", err)
	}
	if _, err := Validate(ctx, client, testSecret, signed); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if _, err := Rotate(ctx, client, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := Issue(ctx, client, testSecret, 3600, Grant{UserID: 7, Epoch: epoch}); !errors.Is(err, ErrEpochMoved) {
		t.Fatalf("Issue() after another rotation: error = %v, want ErrEpochMoved", err)
	}
}

func TestIssueAndValidateFailClosedWithoutStore(t *testing.T) {
	var client *redis.Client
	if _, err := Issue(context.Background(), client, testSecret, 3600, Grant{UserID: 7}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Issue() error = %v, want ErrUnavailable", err)
	}
	if _, err := Validate(context.Background(), client, testSecret, "token"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Validate() error = %v, want ErrUnavailable", err)
	}
	_, live := newTestClient(t)
	if _, err := Issue(context.Background(), live, testSecret, 0, Grant{UserID: 7}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Issue() without a lifetime: error = %v, want ErrUnavailable", err)
	}
}
