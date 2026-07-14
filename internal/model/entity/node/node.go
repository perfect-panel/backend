package node

import (
	"time"

	"github.com/perfect-panel/server/pkg/logger"
	"gorm.io/gorm"
)

type Node struct {
	Id        int64     `gorm:"primary_key"`
	Name      string    `gorm:"type:varchar(100);not null;default:'';comment:Node Name"`
	Tags      string    `gorm:"type:varchar(255);not null;default:'';comment:Tags"`
	Port      uint16    `gorm:"not null;default:0;comment:Connect Port"`
	Address   string    `gorm:"type:varchar(255);not null;default:'';comment:Connect Address"`
	ServerId  int64     `gorm:"not null;default:0;comment:Server ID"`
	Server    *Server   `gorm:"foreignKey:ServerId;references:Id"`
	Protocol  string    `gorm:"type:varchar(100);not null;default:'';comment:Protocol"`
	Enabled   *bool     `gorm:"type:boolean;not null;default:true;comment:Enabled"`
	Sort      int       `gorm:"uniqueIndex;not null;default:0;comment:Sort"`
	CreatedAt time.Time `gorm:"<-:create;comment:Creation Time"`
	UpdatedAt time.Time `gorm:"comment:Update Time"`
}

func (n *Node) TableName() string {
	return "nodes"
}

func (n *Node) BeforeCreate(tx *gorm.DB) error {
	if n.Sort == 0 {
		var maxSort int
		if err := tx.Model(&Node{}).Select("COALESCE(MAX(sort), 0)").Scan(&maxSort).Error; err != nil {
			return err
		}
		n.Sort = maxSort + 1
	}
	return nil
}

func (n *Node) BeforeDelete(tx *gorm.DB) error {
	if err := tx.Exec("UPDATE nodes SET sort = sort - 1 WHERE sort > ?", n.Sort).Error; err != nil {
		return err
	}
	return nil
}

func (n *Node) BeforeUpdate(tx *gorm.DB) error {
	var count int64
	if err := tx.Set("gorm:query_option", "FOR UPDATE").Model(&Node{}).
		Where("sort = ? AND id != ?", n.Sort, n.Id).Count(&count).Error; err != nil {
		return err
	}
	if count > 1 {
		// reorder sort
		if err := reorderSortWithNode(tx); err != nil {
			logger.Errorf("[Node] BeforeUpdate reorderSort error: %v", err.Error())
			return err
		}
		// get max sort
		var maxSort int
		if err := tx.Model(&Node{}).Select("MAX(sort)").Scan(&maxSort).Error; err != nil {
			return err
		}
		n.Sort = maxSort + 1
	}
	return nil
}

func reorderSortWithNode(tx *gorm.DB) error {
	var nodes []Node
	if err := tx.Order("sort, id").Find(&nodes).Error; err != nil {
		return err
	}
	for i, node := range nodes {
		if node.Sort != i+1 {
			if err := tx.Exec("UPDATE nodes SET sort = ? WHERE id = ?", i+1, node.Id).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
