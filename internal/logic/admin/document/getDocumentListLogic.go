package document

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetDocumentListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get document list
func NewGetDocumentListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetDocumentListLogic {
	return &GetDocumentListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetDocumentListLogic) GetDocumentList(req *dto.GetDocumentListRequest) (resp *dto.GetDocumentListResponse, err error) {
	total, data, err := l.svcCtx.Store.Document().QueryDocumentList(l.ctx, int(req.Page), int(req.Size), req.Tag, req.Search)
	if err != nil {
		l.Errorw("[GetDocumentList] Database Error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "QueryDocumentList error: %v", err.Error())
	}
	resp = &dto.GetDocumentListResponse{
		Total: total,
		List:  make([]dto.Document, 0),
	}
	for _, v := range data {
		resp.List = append(resp.List, dto.Document{
			Id:        v.Id,
			Title:     v.Title,
			Tags:      tool.StringMergeAndRemoveDuplicates(v.Tags),
			Content:   v.Content,
			Show:      *v.Show,
			CreatedAt: v.CreatedAt.UnixMilli(),
			UpdatedAt: v.UpdatedAt.UnixMilli(),
		})
	}
	return
}
