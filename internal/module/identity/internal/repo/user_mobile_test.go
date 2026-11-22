package repo

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

// Accounts the admin panel created stored their number as "<area>-<number>"
// and could not sign in by phone. The startup fix-up moves every stored
// number that names its country to E.164, leaves a number alone when its
// E.164 form belongs to another binding, and changes nothing on a second run.
//
// A number without a country is left alone and reported: "15012345678" is a
// Chinese number in national form, and putting "+" in front of it would hand
// the account to the holder of that US number. So is a number whose E.164
// form is not a valid number.
func TestNormalizeMobileIdentifiersConvertsLegacyNumbers(t *testing.T) {
	logs := logtest.NewCollector(t)
	db, repo := newSQLiteUserRepo(t, "normalize-mobile-identifiers")
	ctx := context.Background()

	bindings := map[string]*user.AuthMethods{
		"legacy":    {AuthType: "mobile", AuthIdentifier: "86-15012345678"},
		"spaced":    {AuthType: "mobile", AuthIdentifier: "+86 139 0013 9000"},
		"national":  {AuthType: "mobile", AuthIdentifier: "15012345678"},
		"no plus":   {AuthType: "mobile", AuthIdentifier: "8613900139001"},
		"invalid":   {AuthType: "mobile", AuthIdentifier: "86-12345"},
		"conflict":  {AuthType: "mobile", AuthIdentifier: "86-13700137000"},
		"holder":    {AuthType: "mobile", AuthIdentifier: "+8613700137000"},
		"canonical": {AuthType: "mobile", AuthIdentifier: "+8613600136000"},
		"garbage":   {AuthType: "mobile", AuthIdentifier: "call-me"},
		"device":    {AuthType: "device", AuthIdentifier: "device-86-1"},
	}
	for _, binding := range bindings {
		owner := &user.User{}
		if err := db.Create(owner).Error; err != nil {
			t.Fatal(err)
		}
		binding.UserId = owner.Id
		if err := db.Create(binding).Error; err != nil {
			t.Fatal(err)
		}
	}

	result, err := repo.NormalizeMobileIdentifiers(ctx)
	if err != nil {
		t.Fatalf("NormalizeMobileIdentifiers() error = %v", err)
	}
	if want := (repository.MobileNormalization{Converted: 2, Conflicts: 1, Unparsable: 4}); result != want {
		t.Fatalf("result = %+v, want %+v", result, want)
	}
	for name, want := range map[string]string{
		"legacy":    "+8615012345678",
		"spaced":    "+8613900139000",
		"national":  "15012345678",
		"no plus":   "8613900139001",
		"invalid":   "86-12345",
		"conflict":  "86-13700137000",
		"holder":    "+8613700137000",
		"canonical": "+8613600136000",
		"garbage":   "call-me",
		"device":    "device-86-1",
	} {
		var stored user.AuthMethods
		if err := db.First(&stored, bindings[name].Id).Error; err != nil {
			t.Fatal(err)
		}
		if stored.AuthIdentifier != want {
			t.Errorf("%s binding = %q, want %q", name, stored.AuthIdentifier, want)
		}
	}
	// The rows left alone are reported for the operator, by id and with the
	// number masked.
	for name := range map[string]struct{}{"national": {}, "no plus": {}, "invalid": {}, "garbage": {}} {
		binding := bindings[name]
		if content := logs.String(); strings.Contains(content, binding.AuthIdentifier) || !strings.Contains(content, `"auth_method_id":`+strconv.FormatInt(binding.Id, 10)) {
			t.Errorf("%s binding: log = %s, want the row reported with its number masked", name, content)
		}
	}

	// The converted account now signs in with the number in any form.
	found, err := repo.FindUserAuthMethodByOpenID(ctx, "mobile", "86-15012345678")
	if err != nil || found.UserId != bindings["legacy"].UserId {
		t.Fatalf("lookup after conversion = %+v, %v", found, err)
	}

	again, err := repo.NormalizeMobileIdentifiers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again.Converted != 0 {
		t.Fatalf("second run converted %d bindings, want none", again.Converted)
	}
}

// legacyMobileE164 converts only a stored form that names its country and
// gives a valid number.
func TestLegacyMobileE164(t *testing.T) {
	for stored, want := range map[string]string{
		"86-15012345678":       "+8615012345678",
		" 86 - 150 1234 5678 ": "+8615012345678",
		"1-4407941888":         "+14407941888",
		"+86 150 1234 5678":    "+8615012345678",
		"+86-150-1234-5678":    "+8615012345678",
		"+1 (440) 794-1888":    "+14407941888",
		"+8615012345678":       "+8615012345678",
		"15012345678":          "", // national form: no country to convert with
		"8615012345678":        "", // no "+": a country code cannot be told from the number
		"86 15012345678":       "",
		"86-12345":             "", // not a valid number
		"999-15012345678":      "", // not a country code
		"+86 150 1234 56x8":    "",
		"call-me":              "",
		"-15012345678":         "",
		"":                     "",
	} {
		got, ok := legacyMobileE164(stored)
		if ok != (want != "") || got != want {
			t.Errorf("legacyMobileE164(%q) = %q, %t; want %q", stored, got, ok, want)
		}
	}
}

func TestMaskStoredIdentifier(t *testing.T) {
	for value, want := range map[string]string{
		"15012345678":       "150******78",
		"+86 150 1234 5678": "+86************78",
		"call-me":           "cal**me",
		"12345":             "*****",
		"":                  "",
	} {
		if got := maskStoredIdentifier(value); got != want {
			t.Errorf("maskStoredIdentifier(%q) = %q, want %q", value, got, want)
		}
	}
}
