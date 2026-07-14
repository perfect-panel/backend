package common

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetStatLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Tos
func NewGetStatLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetStatLogic {
	return &GetStatLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetStatLogic) GetStat() (resp *dto.GetStatResponse, err error) {
	respJson, err := l.svcCtx.Redis.Get(l.ctx, config.CommonStatCacheKey).Result()
	if err == nil {
		cachedResp := &dto.GetStatResponse{}
		err = json.Unmarshal([]byte(respJson), cachedResp)
		if err == nil {
			return cachedResp, nil
		}
	}
	userStore := l.svcCtx.Store.User()
	nodeStore := l.svcCtx.Store.Node()
	u, err := userStore.CountEnabledUsers(l.ctx)
	if err != nil {
		l.Logger.Error("[GetStatLogic] get user count failed: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get user count failed: %v", err.Error())
	}
	if u > 100 {
		u -= u % 100
	} else if u > 10 {
		u -= u % 10
	} else {
		u = 1
	}
	n, err := nodeStore.CountEnabledNodes(l.ctx)
	if err != nil {
		l.Logger.Error("[GetStatLogic] get server count failed: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get server count failed: %v", err.Error())
	}
	nodeaddr, err := nodeStore.QueryServerAddresses(l.ctx)
	if err != nil {
		l.Logger.Error("[GetStatLogic] get server_addr failed: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get server_addr failed: %v", err.Error())
	}
	type apireq struct {
		Query  string `json:"query"`
		Fields string `json:"fields"`
	}
	type apiret struct {
		CountryCode string `json:"countryCode"`
	}
	//map as dict
	type void struct{}
	var v void
	country := make(map[string]void)
	for c := range slices.Chunk(nodeaddr, 100) {
		var batchreq []apireq
		for _, addr := range c {
			isAddr := net.ParseIP(addr)
			if isAddr == nil {
				ip, err := net.LookupIP(addr)
				if err == nil && len(ip) > 0 {
					batchreq = append(batchreq, apireq{Query: ip[0].String(), Fields: "countryCode"})
				}
			} else {
				batchreq = append(batchreq, apireq{Query: addr, Fields: "countryCode"})
			}
		}
		if len(batchreq) == 0 {
			continue
		}
		req, _ := json.Marshal(batchreq)
		ret, err := http.Post("http://ip-api.com/batch", "application/json", strings.NewReader(string(req)))
		if err == nil {
			retBytes, err := io.ReadAll(ret.Body)
			_ = ret.Body.Close()
			if err == nil {
				var retStruct []apiret
				err := json.Unmarshal(retBytes, &retStruct)
				if err == nil {
					for _, dat := range retStruct {
						if dat.CountryCode != "" {
							country[dat.CountryCode] = v
						}
					}
				}
			}
		}
	}
	protocolDict := make(map[string]void)
	protocol, err := nodeStore.QueryEnabledNodeProtocols(l.ctx)
	if err != nil {
		l.Logger.Error("[GetStatLogic] get protocol failed: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get protocol failed: %v", err.Error())
	}

	for _, p := range protocol {
		if p == "" {
			continue
		}
		protocolDict[p] = v
	}
	protocol = nil
	for p := range protocolDict {
		protocol = append(protocol, p)
	}
	resp = &dto.GetStatResponse{
		User:     u,
		Node:     n,
		Country:  int64(len(country)),
		Protocol: protocol,
	}
	val, _ := json.Marshal(*resp)
	_ = l.svcCtx.Redis.Set(l.ctx, config.CommonStatCacheKey, string(val), time.Duration(3600)*time.Second).Err()
	return resp, nil
}
