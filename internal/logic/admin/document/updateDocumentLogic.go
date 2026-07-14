package document

import (
	"context"
	"strings"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/document"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateDocumentLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update document
func NewUpdateDocumentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateDocumentLogic {
	return &UpdateDocumentLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateDocumentLogic) UpdateDocument(req *dto.UpdateDocumentRequest) error {
	if err := l.svcCtx.Store.Document().Update(l.ctx, &document.Document{
		Id:      req.Id,
		Title:   req.Title,
		Content: req.Content,
		Tags:    strings.Join(req.Tags, ","),
		Show:    req.Show,
	}); err != nil {
		l.Errorw("[UpdateDocument] Database Error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "failed to update document: %v", err.Error())
	}
	return nil
}
