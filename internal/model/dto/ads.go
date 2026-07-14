package dto

type Ads struct {
	Id          int    `json:"id"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	Content     string `json:"content"`
	Description string `json:"description"`
	TargetURL   string `json:"target_url"`
	StartTime   int64  `json:"start_time"`
	EndTime     int64  `json:"end_time"`
	Status      int    `json:"status"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type CreateAdsRequest struct {
	Title       string `json:"title"`
	Type        string `json:"type"`
	Content     string `json:"content"`
	Description string `json:"description"`
	TargetURL   string `json:"target_url"`
	StartTime   int64  `json:"start_time"`
	EndTime     int64  `json:"end_time"`
	Status      int    `json:"status"`
}

type DeleteAdsRequest struct {
	Id int64 `json:"id"`
}

type GetAdsDetailRequest struct {
	Id int64 `form:"id"`
}

type GetAdsListRequest struct {
	Page   int    `form:"page" validate:"required,gt=0"`
	Size   int    `form:"size" validate:"required,gt=0,lte=100"`
	Status *int   `form:"status,omitempty"`
	Search string `form:"search,omitempty"`
}

type GetAdsListResponse struct {
	Total int64 `json:"total"`
	List  []Ads `json:"list"`
}

type GetAdsRequest struct {
	Device   string `form:"device"`
	Position string `form:"position"`
}

type GetAdsResponse struct {
	List []Ads `json:"list"`
}

type UpdateAdsRequest struct {
	Id          int64  `json:"id"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	Content     string `json:"content"`
	Description string `json:"description"`
	TargetURL   string `json:"target_url"`
	StartTime   int64  `json:"start_time"`
	EndTime     int64  `json:"end_time"`
	Status      int    `json:"status"`
}
