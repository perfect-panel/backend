package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/logger/logtest"
	"github.com/perfect-panel/server/pkg/tool"
)

type passwordUpgradeUserRepo struct {
	repository.UserRepo
	calls       int
	currentHash string
	password    string
	algo        string
	salt        string
	updated     bool
}

func (r *passwordUpgradeUserRepo) UpgradePasswordHash(_ context.Context, _ int64, currentHash, password, algo, salt string) (bool, error) {
	r.calls++
	r.currentHash = currentHash
	r.password = password
	r.algo = algo
	r.salt = salt
	return r.updated, nil
}

func TestUpgradePasswordAfterLoginRehashesLegacyHash(t *testing.T) {
	logtest.Discard(t)
	legacyHash := "$pbkdf2-sha512$xt6kLSoyy7cKFzfY$0cb7525bd89b0f9b4cbfaff942d43dac939ad4ac9aaab504de1971cd31222ad0"
	userInfo := &user.User{Id: 1, Password: legacyHash, Algo: "default"}
	repo := &passwordUpgradeUserRepo{updated: true}
	ctx := context.Background()

	upgradePasswordAfterLogin(ctx, repo, logger.WithContext(ctx), userInfo, "password")

	if repo.calls != 1 {
		t.Fatalf("UpgradePasswordHash calls = %d, want 1", repo.calls)
	}
	if repo.algo != tool.PasswordAlgoArgon2id || repo.salt != "" {
		t.Fatalf("updated algo/salt = %q/%q", repo.algo, repo.salt)
	}
	if repo.currentHash != legacyHash {
		t.Fatalf("current hash = %q, want legacy hash", repo.currentHash)
	}
	if !strings.HasPrefix(repo.password, "$argon2id$") {
		t.Fatalf("updated password is not argon2id PHC: %q", repo.password)
	}
	if !tool.MultiPasswordVerify(tool.PasswordAlgoArgon2id, "", "password", userInfo.Password) {
		t.Fatal("upgraded user password should verify")
	}
}

func TestUpgradePasswordAfterLoginSkipsCurrentHash(t *testing.T) {
	logtest.Discard(t)
	hash := tool.EncodePassWord("password")
	userInfo := &user.User{Id: 1, Password: hash, Algo: tool.PasswordAlgoArgon2id}
	repo := &passwordUpgradeUserRepo{updated: true}
	ctx := context.Background()

	upgradePasswordAfterLogin(ctx, repo, logger.WithContext(ctx), userInfo, "password")

	if repo.calls != 0 {
		t.Fatalf("UpgradePasswordHash calls = %d, want 0", repo.calls)
	}
}

func TestUpgradePasswordAfterLoginKeepsConcurrentPasswordChange(t *testing.T) {
	logtest.Discard(t)
	legacyHash := "$pbkdf2-sha512$xt6kLSoyy7cKFzfY$0cb7525bd89b0f9b4cbfaff942d43dac939ad4ac9aaab504de1971cd31222ad0"
	userInfo := &user.User{Id: 1, Password: legacyHash, Algo: "default"}
	repo := &passwordUpgradeUserRepo{updated: false}
	ctx := context.Background()

	upgradePasswordAfterLogin(ctx, repo, logger.WithContext(ctx), userInfo, "password")

	if repo.calls != 1 {
		t.Fatalf("UpgradePasswordHash calls = %d, want 1", repo.calls)
	}
	if userInfo.Password != legacyHash || userInfo.Algo != "default" {
		t.Fatal("the in-memory user must remain unchanged when the conditional update loses a race")
	}
}
