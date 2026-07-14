package client

import (
	"encoding/json"
	"time"
)

type SubscribeApplication struct {
	Id                int64     `gorm:"primaryKey"`
	Name              string    `gorm:"type:varchar(255);default:'';not null;comment:Application Name"`
	Icon              string    `gorm:"type:MEDIUMTEXT;default:null;comment:Application Icon"`
	Description       string    `gorm:"type:varchar(255);default:null;comment:Application Description"`
	Scheme            string    `gorm:"type:varchar(255);default:'';not null;comment:Scheme"`
	UserAgent         string    `gorm:"type:varchar(255);default:'';not null;comment:User Agent"`
	IsDefault         bool      `gorm:"type:tinyint(1);not null;default:0;comment:Is Default Application"`
	SubscribeTemplate string    `gorm:"type:MEDIUMTEXT;default:null;comment:Subscribe Template"`
	OutputFormat      string    `gorm:"type:varchar(50);default:'yaml';not null;comment:Output Format"`
	DownloadLink      string    `gorm:"type:text;not null;comment:Download Link"`
	CreatedAt         time.Time `gorm:"<-:create;comment:Create Time"`
	UpdatedAt         time.Time `gorm:"comment:Update Time"`
}

func (SubscribeApplication) TableName() string {
	return "subscribe_application"
}

type DownloadLink struct {
	IOS     string `json:"ios,omitempty"`
	Android string `json:"android,omitempty"`
	Windows string `json:"windows,omitempty"`
	Mac     string `json:"mac,omitempty"`
	Linux   string `json:"linux,omitempty"`
	Harmony string `json:"harmony,omitempty"`
}

// GetDownloadLink returns the download link for the specified platform.
func (d *DownloadLink) GetDownloadLink(platform string) string {
	if d == nil {
		return ""
	}
	switch platform {
	case "ios":
		return d.IOS
	case "android":
		return d.Android
	case "windows":
		return d.Windows
	case "mac":
		return d.Mac
	case "linux":
		return d.Linux
	case "harmony":
		return d.Harmony
	default:
		return ""
	}
}

// Marshal serializes the DownloadLink to JSON format.
func (d *DownloadLink) Marshal() ([]byte, error) {
	if d == nil {
		var empty DownloadLink
		return json.Marshal(empty)
	}
	return json.Marshal(d)
}

// Unmarshal parses the JSON-encoded data and stores the result in the DownloadLink.
func (d *DownloadLink) Unmarshal(data []byte) error {
	if data == nil || len(data) == 0 {
		*d = DownloadLink{}
		return nil
	}
	return json.Unmarshal(data, d)
}
