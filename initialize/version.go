package initialize

import (
	"context"
	"errors"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"

	"github.com/perfect-panel/server/initialize/migrate"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/orm"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
)

func Migrate(ctx *svc.ServiceContext) {
	mc := orm.Mysql{
		Config: ctx.Config.DatabaseConfig(),
	}
	now := time.Now()
	if err := migrate.Migrate(mc.Driver(), mc.MigrationDsn()).Up(); err != nil {
		if errors.Is(err, migrate.NoChange) {
			logger.Info("[Migrate] database not change")
			return
		}
		logger.Errorf("[Migrate] Up error: %v", err.Error())
		panic(err)
	} else {
		logger.Info("[Migrate] Database change, took " + time.Since(now).String())
	}
	// if not found admin user
	err := ctx.Store.InTx(context.Background(), func(store repository.Store) error {
		count, err := store.User().QueryResisterUserTotal(context.Background())
		if err != nil {
			return err
		}
		if count == 0 {
			enable := true
			admin := &user.User{
				Password:  tool.EncodePassWord(ctx.Config.Administrator.Password),
				Algo:      tool.PasswordAlgoArgon2id,
				IsAdmin:   &enable,
				ReferCode: uuidx.UserInviteCode(time.Now().Unix()),
			}
			if err := store.User().Insert(context.Background(), admin); err != nil {
				logger.Errorf("[Migrate] CreateAdminUser error: %v", err.Error())
				return err
			}
			if err := store.UserAuth().InsertUserAuthMethods(context.Background(), &user.AuthMethods{
				UserId:         admin.Id,
				AuthType:       "email",
				AuthIdentifier: ctx.Config.Administrator.Email,
				Verified:       true,
			}); err != nil {
				logger.Errorf("[Migrate] CreateAdminUser error: %v", err.Error())
				return err
			}
			logger.Info("[Migrate] Create admin user success")
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
}
