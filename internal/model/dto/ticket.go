package dto

type CreateTicketFollowRequest struct {
	TicketId int64  `json:"ticket_id" validate:"required"`
	From     string `json:"from" validate:"required"`
	Type     uint8  `json:"type" validate:"required"`
	Content  string `json:"content" validate:"required"`
}

type CreateUserTicketFollowRequest struct {
	TicketId int64  `json:"ticket_id"`
	From     string `json:"from"`
	Type     uint8  `json:"type"`
	Content  string `json:"content"`
}

type CreateUserTicketRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type Follow struct {
	Id        int64  `json:"id"`
	TicketId  int64  `json:"ticket_id"`
	From      string `json:"from"`
	Type      uint8  `json:"type"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
}

type GetTicketListRequest struct {
	Page   int64  `form:"page" validate:"required,gt=0"`
	Size   int64  `form:"size" validate:"required,gt=0,lte=100"`
	UserId int64  `form:"user_id,omitempty"`
	Status *uint8 `form:"status,omitempty"`
	Search string `form:"search,omitempty"`
}

type GetTicketListResponse struct {
	Total int64    `json:"total"`
	List  []Ticket `json:"list"`
}

type GetTicketRequest struct {
	Id int64 `form:"id" validate:"required"`
}

type GetUserTicketDetailRequest struct {
	Id int64 `form:"id" validate:"required"`
}

type GetUserTicketListRequest struct {
	Page   int    `form:"page" validate:"required,gt=0"`
	Size   int    `form:"size" validate:"required,gt=0,lte=100"`
	Status *uint8 `form:"status,omitempty"`
	Search string `form:"search,omitempty"`
}

type GetUserTicketListResponse struct {
	Total int64    `json:"total"`
	List  []Ticket `json:"list"`
}

type Ticket struct {
	Id          int64    `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	UserId      int64    `json:"user_id"`
	Follows     []Follow `json:"follow,omitempty"`
	Status      uint8    `json:"status"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
}

type TicketWaitRelpyResponse struct {
	Count int64 `json:"count"`
}

type UpdateTicketStatusRequest struct {
	Id     int64  `json:"id" validate:"required"`
	Status *uint8 `json:"status" validate:"required"`
}

type UpdateUserTicketStatusRequest struct {
	Id     int64  `json:"id" validate:"required"`
	Status *uint8 `json:"status" validate:"required"`
}
