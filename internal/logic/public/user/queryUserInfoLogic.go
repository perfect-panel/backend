package user

import (
	"context"
	"sort"

	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/phone"
	"github.com/perfect-panel/server/pkg/tool"
)

type QueryUserInfoLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Query User Info
func NewQueryUserInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryUserInfoLogic {
	return &QueryUserInfoLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryUserInfoLogic) QueryUserInfo() (resp *dto.User, err error) {
	resp = &dto.User{}
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	tool.DeepCopy(resp, u)

	var userMethods []dto.UserAuthMethod
	for _, method := range resp.AuthMethods {
		var item dto.UserAuthMethod
		tool.DeepCopy(&item, method)

		switch method.AuthType {
		case "mobile":
			item.AuthIdentifier = phone.MaskPhoneNumber(method.AuthIdentifier)
		case "email":
		default:
			item.AuthIdentifier = maskOpenID(method.AuthIdentifier)
		}
		userMethods = append(userMethods, item)
	}

	// 按照指定顺序排序：email第一位，mobile第二位，其他按原顺序
	sort.Slice(userMethods, func(i, j int) bool {
		return getAuthTypePriority(userMethods[i].AuthType) < getAuthTypePriority(userMethods[j].AuthType)
	})

	resp.AuthMethods = userMethods
	return resp, nil
}

// getAuthTypePriority 获取认证类型的排序优先级
// email: 1 (第一位)
// mobile: 2 (第二位)
// 其他类型: 100+ (后续位置)
func getAuthTypePriority(authType string) int {
	switch authType {
	case "email":
		return 1
	case "mobile":
		return 2
	default:
		return 100
	}
}

// maskOpenID 脱敏 OpenID，只保留前 3 和后 3 位
func maskOpenID(openID string) string {
	length := len(openID)
	if length <= 6 {
		return "***" // 如果 ID 太短，直接返回 "***"
	}

	// 计算中间需要被替换的 `*` 数量
	maskLength := length - 6
	mask := make([]byte, maskLength)
	for i := range mask {
		mask[i] = '*'
	}

	// 组合脱敏后的 OpenID
	return openID[:3] + string(mask) + openID[length-3:]
}
