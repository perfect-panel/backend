package dto

type BatchDeleteDocumentRequest struct {
	Ids []int64 `json:"ids" validate:"required"`
}

type CreateDocumentRequest struct {
	Title   string   `json:"title" validate:"required"`
	Content string   `json:"content" validate:"required"`
	Tags    []string `json:"tags,omitempty" `
	Show    *bool    `json:"show"`
}

type DeleteDocumentRequest struct {
	Id int64 `json:"id" validate:"required"`
}

type Document struct {
	Id        int64    `json:"id"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags"`
	Show      bool     `json:"show"`
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
}

type GetDocumentDetailRequest struct {
	Id int64 `json:"id" validate:"required"`
}

type GetDocumentListRequest struct {
	Page   int64  `form:"page" validate:"required,gt=0"`
	Size   int64  `form:"size" validate:"required,gt=0,lte=100"`
	Tag    string `form:"tag,omitempty"`
	Search string `form:"search,omitempty"`
}

type GetDocumentListResponse struct {
	Total int64      `json:"total"`
	List  []Document `json:"list"`
}

type QueryDocumentDetailRequest struct {
	Id int64 `form:"id" validate:"required"`
}

type QueryDocumentListResponse struct {
	Total int64      `json:"total"`
	List  []Document `json:"list"`
}

type UpdateDocumentRequest struct {
	Id      int64    `json:"id" validate:"required"`
	Title   string   `json:"title" validate:"required"`
	Content string   `json:"content" validate:"required"`
	Tags    []string `json:"tags,omitempty" `
	Show    *bool    `json:"show"`
}
