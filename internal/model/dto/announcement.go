package dto

type Announcement struct {
	Id        int64  `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Show      *bool  `json:"show"`
	Pinned    *bool  `json:"pinned"`
	Popup     *bool  `json:"popup"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type CreateAnnouncementRequest struct {
	Title   string `json:"title" validate:"required"`
	Content string `json:"content" validate:"required"`
}

type DeleteAnnouncementRequest struct {
	Id int64 `json:"id" validate:"required"`
}

type GetAnnouncementListRequest struct {
	Page   int64  `form:"page" validate:"required,gt=0"`
	Size   int64  `form:"size" validate:"required,gt=0,lte=100"`
	Show   *bool  `form:"show,omitempty"`
	Pinned *bool  `form:"pinned,omitempty"`
	Popup  *bool  `form:"popup,omitempty"`
	Search string `form:"search,omitempty"`
}

type GetAnnouncementListResponse struct {
	Total int64          `json:"total"`
	List  []Announcement `json:"list"`
}

type GetAnnouncementRequest struct {
	Id int64 `form:"id" validate:"required"`
}

type QueryAnnouncementRequest struct {
	Page   int   `form:"page" validate:"required,gt=0"`
	Size   int   `form:"size" validate:"required,gt=0,lte=100"`
	Pinned *bool `form:"pinned"`
	Popup  *bool `form:"popup"`
}

type QueryAnnouncementResponse struct {
	Total int64          `json:"total"`
	List  []Announcement `json:"announcements"`
}

type UpdateAnnouncementEnableRequest struct {
	Id     int64 `json:"id" validate:"required"`
	Enable *bool `json:"enable" validate:"required"`
}

type UpdateAnnouncementRequest struct {
	Id      int64  `json:"id" validate:"required"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Show    *bool  `json:"show"`
	Pinned  *bool  `json:"pinned"`
	Popup   *bool  `json:"popup"`
}
