package repo

import (
	"strings"
	"testing"

	trafficEntity "github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestUserSubscribeTrafficIncrementExprSQL(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
		want      []string
	}{
		{
			name: "mysql",
			dialector: mysql.New(mysql.Config{
				DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
				SkipInitializeWithVersion: true,
			}),
			want: []string{
				"UPDATE `user_subscribe`",
				"`download`=`user_subscribe`.`download` + CASE `user_subscribe`.`id` WHEN ? THEN ? WHEN ? THEN ? ELSE CAST(0 AS SIGNED) END",
				"`upload`=`user_subscribe`.`upload` + CASE `user_subscribe`.`id` WHEN ? THEN ? WHEN ? THEN ? ELSE CAST(0 AS SIGNED) END",
				"WHERE id IN (?,?)",
			},
		},
		{
			name: "postgres",
			dialector: postgres.New(postgres.Config{
				DSN:                  "host=localhost user=gorm password=gorm dbname=gorm port=9920 sslmode=disable",
				PreferSimpleProtocol: true,
			}),
			want: []string{
				`UPDATE "user_subscribe"`,
				`"download"="user_subscribe"."download" + CASE "user_subscribe"."id" WHEN $1 THEN $2 WHEN $3 THEN $4 ELSE 0::bigint END`,
				`"upload"="user_subscribe"."upload" + CASE "user_subscribe"."id" WHEN $5 THEN $6 WHEN $7 THEN $8 ELSE 0::bigint END`,
				`WHERE id IN ($10,$11)`,
			},
		},
	}

	deltas := mergeSubscribeTrafficDeltas([]trafficEntity.SubscribeTrafficDelta{
		{SubscribeId: 2, Download: 20, Upload: 10},
		{SubscribeId: 1, Download: 2_508_079_104, Upload: 30},
		{SubscribeId: 2, Download: 3, Upload: 4},
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := gorm.Open(tt.dialector, &gorm.Config{
				DryRun:                 true,
				DisableAutomaticPing:   true,
				SkipDefaultTransaction: true,
			})
			if err != nil {
				t.Fatalf("open gorm db: %v", err)
			}

			conn := db.Model(&usersub.Subscribe{})
			downloadExpr, downloadArgs := userSubscribeTrafficIncrementExpr(conn, "download", deltas)
			uploadExpr, uploadArgs := userSubscribeTrafficIncrementExpr(conn, "upload", deltas)
			stmt := conn.Where("id IN ?", []int64{1, 2}).Updates(map[string]any{
				"download": gorm.Expr(downloadExpr, downloadArgs...),
				"upload":   gorm.Expr(uploadExpr, uploadArgs...),
			}).Statement
			sql := stmt.SQL.String()
			for _, want := range tt.want {
				if !strings.Contains(sql, want) {
					t.Fatalf("SQL missing %q:\n%s", want, sql)
				}
			}
			if got := stmt.Vars[1]; got != int64(2_508_079_104) {
				t.Fatalf("first download increment = %#v, want 2508079104", got)
			}
			if got := stmt.Vars[3]; got != int64(23) {
				t.Fatalf("second download increment = %#v, want 23", got)
			}
		})
	}
}
