package repo

import (
	"strings"
	"testing"

	"github.com/perfect-panel/server/internal/module/subscription/entity/usersub"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestSubscribeFilterHonorsBothActiveValues(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}

	active := true
	var rows []*usersub.Subscribe
	activeStmt := subscribeFilterQuery(db, &usersub.SubscribeFilter{IsActive: &active}).Find(&rows).Statement
	if !strings.Contains(activeStmt.SQL.String(), "status IN (?,?,?)") {
		t.Fatalf("active filter = SQL %s vars %#v", activeStmt.SQL.String(), activeStmt.Vars)
	}

	inactive := false
	inactiveStmt := subscribeFilterQuery(db, &usersub.SubscribeFilter{IsActive: &inactive}).Find(&rows).Statement
	if !strings.Contains(inactiveStmt.SQL.String(), "status IN (?,?)") {
		t.Fatalf("inactive filter = SQL %s vars %#v", inactiveStmt.SQL.String(), inactiveStmt.Vars)
	}

	unfilteredSQL := subscribeFilterQuery(db, &usersub.SubscribeFilter{}).Find(&rows).Statement.SQL.String()
	if !strings.Contains(unfilteredSQL, "status <> ?") {
		t.Fatalf("unfiltered quota scope must exclude deducted subscriptions: %s", unfilteredSQL)
	}
}
