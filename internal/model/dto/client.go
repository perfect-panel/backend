package dto

type Application struct {
	Id            int64  `json:"id"`
	Icon          string `json:"icon"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	SubscribeType string `json:"subscribe_type"`
}

type ApplicationPlatform struct {
	IOS     []*ApplicationVersion `json:"ios,omitempty"`
	MacOS   []*ApplicationVersion `json:"macos,omitempty"`
	Linux   []*ApplicationVersion `json:"linux,omitempty"`
	Android []*ApplicationVersion `json:"android,omitempty"`
	Windows []*ApplicationVersion `json:"windows,omitempty"`
	Harmony []*ApplicationVersion `json:"harmony,omitempty"`
}

type ApplicationResponse struct {
	Applications []ApplicationResponseInfo `json:"applications"`
}

type ApplicationResponseInfo struct {
	Id            int64               `json:"id"`
	Name          string              `json:"name"`
	Icon          string              `json:"icon"`
	Description   string              `json:"description"`
	SubscribeType string              `json:"subscribe_type"`
	Platform      ApplicationPlatform `json:"platform"`
}

type ApplicationVersion struct {
	Id          int64  `json:"id"`
	Url         string `json:"url"`
	Version     string `json:"version" validate:"required"`
	Description string `json:"description"`
	IsDefault   bool   `json:"is_default"`
}

type CreateSubscribeApplicationRequest struct {
	Name              string       `json:"name"`
	Description       string       `json:"description,omitempty"`
	Icon              string       `json:"icon,omitempty"`
	Scheme            string       `json:"scheme,omitempty"`
	UserAgent         string       `json:"user_agent"`
	IsDefault         bool         `json:"is_default"`
	SubscribeTemplate string       `json:"template"`
	OutputFormat      string       `json:"output_format"`
	DownloadLink      DownloadLink `json:"download_link"`
}

type DeleteSubscribeApplicationRequest struct {
	Id int64 `json:"id"`
}

type DownloadLink struct {
	IOS     string `json:"ios,omitempty"`
	Android string `json:"android,omitempty"`
	Windows string `json:"windows,omitempty"`
	Mac     string `json:"mac,omitempty"`
	Linux   string `json:"linux,omitempty"`
	Harmony string `json:"harmony,omitempty"`
}

type GetSubscribeApplicationListRequest struct {
	Page int `form:"page" validate:"required,gt=0"`
	Size int `form:"size" validate:"required,gt=0,lte=100"`
}

type GetSubscribeApplicationListResponse struct {
	Total int64                  `json:"total"`
	List  []SubscribeApplication `json:"list"`
}

type GetSubscribeClientResponse struct {
	Total int64             `json:"total"`
	List  []SubscribeClient `json:"list"`
}

type PreviewSubscribeTemplateRequest struct {
	Id int64 `form:"id"`
}

type PreviewSubscribeTemplateResponse struct {
	Template string `json:"template"` // 预览的模板内容
}

type SubscribeApplication struct {
	Id                int64        `json:"id"`
	Name              string       `json:"name"`
	Description       string       `json:"description,omitempty"`
	Icon              string       `json:"icon,omitempty"`
	Scheme            string       `json:"scheme,omitempty"`
	UserAgent         string       `json:"user_agent"`
	IsDefault         bool         `json:"is_default"`
	SubscribeTemplate string       `json:"template"`
	OutputFormat      string       `json:"output_format"`
	DownloadLink      DownloadLink `json:"download_link,omitempty"`
	CreatedAt         int64        `json:"created_at"`
	UpdatedAt         int64        `json:"updated_at"`
}

type SubscribeClient struct {
	Id           int64        `json:"id"`
	Name         string       `json:"name"`
	Description  string       `json:"description,omitempty"`
	Icon         string       `json:"icon,omitempty"`
	Scheme       string       `json:"scheme,omitempty"`
	IsDefault    bool         `json:"is_default"`
	DownloadLink DownloadLink `json:"download_link,omitempty"`
}

type UpdateSubscribeApplicationRequest struct {
	Id                int64        `json:"id"`
	Name              string       `json:"name"`
	Description       string       `json:"description,omitempty"`
	Icon              string       `json:"icon,omitempty"`
	Scheme            string       `json:"scheme,omitempty"`
	UserAgent         string       `json:"user_agent"`
	IsDefault         bool         `json:"is_default"`
	SubscribeTemplate string       `json:"template"`
	OutputFormat      string       `json:"output_format"`
	DownloadLink      DownloadLink `json:"download_link,omitempty"`
}
