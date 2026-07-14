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

type CreateDocumentLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create document
func NewCreateDocumentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateDocumentLogic {
	return &CreateDocumentLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateDocumentLogic) CreateDocument(req *dto.CreateDocumentRequest) error {
	if err := l.svcCtx.Store.Document().Insert(l.ctx, &document.Document{
		Title:   req.Title,
		Content: req.Content,
		Tags:    strings.Join(req.Tags, ","),
		Show:    req.Show,
	}); err != nil {
		l.Errorw("[CreateDocument] Database Error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert document error: %v", err.Error())
	}
	return nil
}
