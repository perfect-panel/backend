package user

import "testing"

func TestUserCacheKeysCanonicalizeEmail(t *testing.T) {
	userWithMixedCaseEmail := &User{
		Id: 7,
		AuthMethods: []AuthMethods{{
			AuthType:       "email",
			AuthIdentifier: " Alice@Example.COM ",
		}},
	}
	userWithCanonicalEmail := &User{
		Id: 7,
		AuthMethods: []AuthMethods{{
			AuthType:       "email",
			AuthIdentifier: "alice@example.com",
		}},
	}

	mixedCaseKeys := userWithMixedCaseEmail.GetCacheKeys()
	canonicalKeys := userWithCanonicalEmail.GetCacheKeys()
	if len(mixedCaseKeys) != 2 || len(canonicalKeys) != 2 {
		t.Fatalf("email users should have ID and email keys: %#v %#v", mixedCaseKeys, canonicalKeys)
	}
	if mixedCaseKeys[1] != canonicalKeys[1] {
		t.Fatalf("email cache keys differ: %q != %q", mixedCaseKeys[1], canonicalKeys[1])
	}
}

func TestAuthMethodCacheKeysOnlyCreateEmailKeysForEmail(t *testing.T) {
	emailKeys := (&AuthMethods{UserId: 7, AuthType: "email", AuthIdentifier: " Alice@Example.COM "}).GetCacheKeys()
	if len(emailKeys) != 2 || emailKeys[1] != "cache:user:email:v2:alice@example.com" {
		t.Fatalf("email auth method keys = %#v", emailKeys)
	}

	nonEmailKeys := (&AuthMethods{UserId: 7, AuthType: "google", AuthIdentifier: " Alice@Example.COM "}).GetCacheKeys()
	if len(nonEmailKeys) != 1 || nonEmailKeys[0] != "cache:user:id:7" {
		t.Fatalf("non-email auth method keys = %#v", nonEmailKeys)
	}
}

func TestSubscribeCacheKeyUsesV2UserListKey(t *testing.T) {
	keys := (&Subscribe{Id: 8, UserId: 7, Token: "token"}).GetCacheKeys()
	want := "cache:user:subscribe:user:v2:7"
	for _, key := range keys {
		if key == want {
			return
		}
	}
	t.Fatalf("subscription cache keys = %#v, want %q", keys, want)
}
