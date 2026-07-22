package repository

import (
	"context"

	"github.com/perfect-panel/server/internal/model/entity/client"
	"gorm.io/gorm"
)

// ClientRepo subscribe application 数据访问接口
type ClientRepo interface {
	Insert(ctx context.Context, data *client.SubscribeApplication) error
	FindOne(ctx context.Context, id int64) (*client.SubscribeApplication, error)
	Update(ctx context.Context, data *client.SubscribeApplication) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context) ([]*client.SubscribeApplication, error)
}

var _ ClientRepo = (*clientRepo)(nil)

type clientRepo struct {
	*gorm.DB
}

func newClientRepo(db *gorm.DB) ClientRepo {
	return &clientRepo{
		DB: db,
	}
}

func (m *clientRepo) Insert(ctx context.Context, data *client.SubscribeApplication) error {
	if err := m.WithContext(ctx).Model(&client.SubscribeApplication{}).Create(data).Error; err != nil {
		return err
	}
	return nil
}

func (m *clientRepo) FindOne(ctx context.Context, id int64) (*client.SubscribeApplication, error) {
	var resp client.SubscribeApplication
	if err := m.WithContext(ctx).Model(&client.SubscribeApplication{}).Where("id = ?", id).First(&resp).Error; err != nil {
		return nil, err
	}
	return &resp, nil
}

func (m *clientRepo) Update(ctx context.Context, data *client.SubscribeApplication) error {
	if _, err := m.FindOne(ctx, data.Id); err != nil {
		return err
	}
	if err := m.WithContext(ctx).Model(&client.SubscribeApplication{}).Where("id = ?", data.Id).Save(data).Error; err != nil {
		return err
	}
	return nil
}

func (m *clientRepo) Delete(ctx context.Context, id int64) error {
	if err := m.WithContext(ctx).Model(&client.SubscribeApplication{}).Where("id = ?", id).Delete(&client.SubscribeApplication{}).Error; err != nil {
		return err
	}
	return nil
}

func (m *clientRepo) List(ctx context.Context) ([]*client.SubscribeApplication, error) {
	var resp []*client.SubscribeApplication
	if err := m.WithContext(ctx).Find(&resp).Error; err != nil {
		return nil, err
	}
	return resp, nil
}
