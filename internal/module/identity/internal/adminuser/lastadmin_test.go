package adminuser

import (
	"context"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// administrator stores an administrator account, enabled or not.
func (f *fixture) administrator(t *testing.T, enabled bool) *user.User {
	t.Helper()
	isAdmin := true
	u := &user.User{Enable: &enabled, IsAdmin: &isAdmin}
	if err := f.DB.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

// edit is the admin edit of u that keeps everything but the flags given.
func edit(u *user.User, enable, isAdmin bool) *dto.UpdateUserBasicInfoRequest {
	return &dto.UpdateUserBasicInfoRequest{UserId: u.Id, Enable: enable, IsAdmin: isAdmin, ReferCode: u.ReferCode}
}

// The last enabled administrator is neither demoted nor disabled: nobody
// could then administer the panel. Once another enabled administrator
// exists the edit goes through, and a disabled administrator does not
// count as that other one.
func TestTheLastAdministratorCannotBeDemotedOrDisabled(t *testing.T) {
	f := newFixture(t)
	last := f.administrator(t, true)
	f.administrator(t, false)
	f.account(t, "email", "member@example.com")
	ctx := context.Background()

	assertCode(t, f.svc.UpdateUserBasicInfo(ctx, edit(last, true, false)), xerr.InvalidParams)
	assertCode(t, f.svc.UpdateUserBasicInfo(ctx, edit(last, false, true)), xerr.InvalidParams)
	if err := f.svc.UpdateUserBasicInfo(ctx, edit(last, true, true)); err != nil {
		t.Fatalf("an edit keeping the administrator: %v", err)
	}
	var stored user.User
	if err := f.DB.First(&stored, last.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !*stored.IsAdmin || !*stored.Enable {
		t.Fatalf("the last administrator was changed: %+v", stored)
	}

	other := f.administrator(t, true)
	if err := f.svc.UpdateUserBasicInfo(ctx, edit(last, true, false)); err != nil {
		t.Fatalf("demoting one of two administrators: %v", err)
	}
	// Now the other one is the last.
	assertCode(t, f.svc.UpdateUserBasicInfo(ctx, edit(other, false, true)), xerr.InvalidParams)
}

// The last enabled administrator cannot be deleted, alone or in a batch that
// removes every enabled administrator; members and disabled administrators
// can, and so can one administrator of two.
func TestTheLastAdministratorCannotBeDeleted(t *testing.T) {
	f := newFixture(t)
	last := f.administrator(t, true)
	disabled := f.administrator(t, false)
	member := f.account(t, "email", "member@example.com")
	ctx := context.Background()

	assertCode(t, f.svc.DeleteUser(ctx, &dto.GetDetailRequest{Id: last.Id}), xerr.InvalidParams)
	assertCode(t, f.svc.BatchDeleteUser(ctx, &dto.BatchDeleteUserRequest{Ids: []int64{member.Id, last.Id}}), xerr.InvalidParams)
	if err := f.svc.DeleteUser(ctx, &dto.GetDetailRequest{Id: member.Id}); err != nil {
		t.Fatalf("deleting a member: %v", err)
	}
	if err := f.svc.BatchDeleteUser(ctx, &dto.BatchDeleteUserRequest{Ids: []int64{disabled.Id}}); err != nil {
		t.Fatalf("deleting a disabled administrator: %v", err)
	}

	other := f.administrator(t, true)
	if err := f.svc.DeleteUser(ctx, &dto.GetDetailRequest{Id: other.Id}); err != nil {
		t.Fatalf("deleting one of two administrators: %v", err)
	}
	assertCode(t, f.svc.BatchDeleteUser(ctx, &dto.BatchDeleteUserRequest{Ids: []int64{last.Id}}), xerr.InvalidParams)
	var live int64
	f.DB.Model(&user.User{}).Where("is_admin = ? AND enable = ?", true, true).Count(&live)
	if live != 1 {
		t.Fatalf("enabled administrators = %d, want the last one kept", live)
	}
}

// A referral share above the whole of a purchase is refused, on creation and
// on edit.
func TestReferralPercentageIsCappedAtTheWhole(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	assertCode(t, f.svc.CreateUser(ctx, &dto.CreateUserRequest{Email: "new@example.com", ReferralPercentage: 101}), xerr.InvalidParams)
	if n := len(f.Users(t)); n != 0 {
		t.Fatalf("accounts = %d, want none created", n)
	}
	u := f.account(t, "email", "member@example.com")
	assertCode(t, f.svc.UpdateUserBasicInfo(ctx, &dto.UpdateUserBasicInfoRequest{UserId: u.Id, Enable: true, ReferralPercentage: 101}), xerr.InvalidParams)
	if err := f.svc.UpdateUserBasicInfo(ctx, &dto.UpdateUserBasicInfoRequest{UserId: u.Id, Enable: true, ReferralPercentage: 100}); err != nil {
		t.Fatalf("a whole share: %v", err)
	}
}
