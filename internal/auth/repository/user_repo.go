package repository

import (
	"bedrock/internal/auth/model"
	"bedrock/internal/pkg"

	"gorm.io/gorm"
)

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(user *model.User) error {
	return r.db.Create(user).Error
}

func (r *UserRepository) FindByID(id uint) (*model.User, error) {
	var user model.User
	err := r.db.First(&user, id).Error
	return &user, err
}

func (r *UserRepository) FindByUsername(username string) (*model.User, error) {
	var user model.User
	err := r.db.Where("username = ?", username).First(&user).Error
	return &user, err
}

func (r *UserRepository) List(q pkg.ListQuery) ([]model.User, int64, error) {
	var users []model.User
	var total int64
	if err := r.db.Model(&model.User{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.Offset(q.Offset()).Limit(q.PageSize).Order("id DESC").Find(&users).Error
	return users, total, err
}

func (r *UserRepository) Update(user *model.User) error {
	return r.db.Save(user).Error
}

func (r *UserRepository) Delete(id uint) error {
	return r.db.Delete(&model.User{}, id).Error
}
