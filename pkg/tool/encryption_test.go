package tool

import (
	"strings"
	"testing"
)

func TestEncodePassWord(t *testing.T) {
	hash := EncodePassWord("password")
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("EncodePassWord prefix = %q", hash)
	}
	if !MultiPasswordVerify(PasswordAlgoArgon2id, "", "password", hash) {
		t.Fatal("argon2id: correct password should verify")
	}
	if MultiPasswordVerify(PasswordAlgoArgon2id, "", "wrong", hash) {
		t.Fatal("argon2id: wrong password must not verify")
	}
	if PasswordNeedsRehash(PasswordAlgoArgon2id, hash) {
		t.Fatal("current argon2id hash should not need rehash")
	}
	if PasswordAlgoForHash(hash) != PasswordAlgoArgon2id {
		t.Fatal("argon2id PHC hash should resolve to argon2id algo")
	}
}

func TestMultiPasswordVerify(t *testing.T) {
	pwd := "$2y$10$WFO17pdtohfeBILjEChoGeVxpDG.u9kVCKhjDAeEeNmCjIlj3tDRy"
	status := MultiPasswordVerify("bcrypt", "", "admin1", pwd)
	t.Logf("MultiPasswordVerify: %v", status)
}

func TestMultiPasswordVerifySha256Salt(t *testing.T) {
	// sha256("123456" + "ppanel")
	hash := "4fb4d5ec8ec384d63cfe1faf2d9610140b310f68fd72eb0df90d3027b702b35f"
	if !MultiPasswordVerify("sha256salt", "ppanel", "123456", hash) {
		t.Fatal("sha256salt: correct password should verify")
	}
	if MultiPasswordVerify("sha256salt", "ppanel", "wrong", hash) {
		t.Fatal("sha256salt: wrong password must not verify")
	}
}

func TestMultiPasswordVerifyLegacyDefaultPBKDF2(t *testing.T) {
	hash := "$pbkdf2-sha512$xt6kLSoyy7cKFzfY$0cb7525bd89b0f9b4cbfaff942d43dac939ad4ac9aaab504de1971cd31222ad0"
	if !MultiPasswordVerify("default", "", "password", hash) {
		t.Fatal("legacy default pbkdf2: correct password should verify")
	}
	if MultiPasswordVerify("default", "", "wrong", hash) {
		t.Fatal("legacy default pbkdf2: wrong password must not verify")
	}
	if !PasswordNeedsRehash("default", hash) {
		t.Fatal("legacy default pbkdf2 hash should need rehash")
	}
	if PasswordAlgoForHash(hash) != "default" {
		t.Fatal("legacy pbkdf2 hash should resolve to default algo")
	}
}

func TestMultiPasswordVerifyMalformedHash(t *testing.T) {
	for _, hash := range []string{"", "not-a-hash", "$pbkdf2-sha512$missing", "$argon2id$v=19$m=999999999,t=2,p=1$a$b"} {
		if MultiPasswordVerify("default", "", "password", hash) {
			t.Fatalf("malformed hash %q verified", hash)
		}
		if !PasswordNeedsRehash("default", hash) {
			t.Fatalf("malformed hash %q should need rehash", hash)
		}
	}
}

func TestPasswordNeedsRehashForNonCurrentArgon2idPHC(t *testing.T) {
	hash := EncodePassWord("password")
	parts := strings.Split(hash, "$")
	parts[4] = "c2hvcnQ" // "short"
	if !PasswordNeedsRehash(PasswordAlgoArgon2id, strings.Join(parts, "$")) {
		t.Fatal("argon2id hash with a short salt should need rehash")
	}

	duplicateParams := strings.Replace(hash, "m=19456,t=2,p=1", "m=19456,m=19456,t=2,p=1", 1)
	if MultiPasswordVerify(PasswordAlgoArgon2id, "", "password", duplicateParams) {
		t.Fatal("argon2id PHC with duplicate parameters must not verify")
	}
}
