package repository

import (
	"time"

	"bedrock/internal/pkg"
	"bedrock/internal/system/model"

	"gorm.io/gorm"
)

// BackupRepository handles database persistence for system backup records.
type BackupRepository struct {
	db *gorm.DB
}

// NewBackupRepository creates a new BackupRepository instance.
func NewBackupRepository(db *gorm.DB) *BackupRepository {
	return &BackupRepository{db: db}
}

// Create inserts a new backup record.
func (r *BackupRepository) Create(b *model.SystemBackup) error {
	return r.db.Create(b).Error
}

// FindByID retrieves a backup record by its primary key ID.
func (r *BackupRepository) FindByID(id uint) (*model.SystemBackup, error) {
	var b model.SystemBackup
	err := r.db.First(&b, id).Error
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// FindByFilename retrieves a backup record by its filename.
func (r *BackupRepository) FindByFilename(filename string) (*model.SystemBackup, error) {
	var b model.SystemBackup
	err := r.db.Where("filename = ?", filename).First(&b).Error
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// List queries backup records with pagination, ordered by created_at DESC, id DESC by default.
func (r *BackupRepository) List(q pkg.ListQuery) ([]model.SystemBackup, int64, error) {
	q = q.Normalize()
	var total int64
	if err := r.db.Model(&model.SystemBackup{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	order := pkg.OrderBy(q.Sort, map[string]string{
		"created_at": "created_at",
		"id":         "id",
		"file_size":  "file_size",
		"status":     "status",
	}, "created_at", "created_at DESC, id DESC")

	var items []model.SystemBackup
	err := r.db.Offset(q.Offset()).Limit(q.PageSize).Order(order).Find(&items).Error
	return items, total, err
}

// Update saves all fields of the given backup record.
func (r *BackupRepository) Update(b *model.SystemBackup) error {
	return r.db.Save(b).Error
}

// UpdateStatus updates the status and error_message of a backup record.
func (r *BackupRepository) UpdateStatus(id uint, status string, errMsg string) error {
	return r.db.Model(&model.SystemBackup{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":        status,
		"error_message": errMsg,
		"updated_at":    time.Now().UTC(),
	}).Error
}

// UpdateSuccess marks a backup record as succeeded with final file size.
func (r *BackupRepository) UpdateSuccess(id uint, fileSize int64) error {
	return r.db.Model(&model.SystemBackup{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":        model.BackupStatusSuccess,
		"file_size":     fileSize,
		"error_message": "",
		"updated_at":    time.Now().UTC(),
	}).Error
}

// UpdateFailed marks a backup record as failed with an error message.
func (r *BackupRepository) UpdateFailed(id uint, errMsg string) error {
	return r.db.Model(&model.SystemBackup{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":        model.BackupStatusFailed,
		"error_message": errMsg,
		"updated_at":    time.Now().UTC(),
	}).Error
}

// UpdateFileSize updates only the file_size field.
func (r *BackupRepository) UpdateFileSize(id uint, size int64) error {
	return r.db.Model(&model.SystemBackup{}).Where("id = ?", id).Updates(map[string]interface{}{
		"file_size":  size,
		"updated_at": time.Now().UTC(),
	}).Error
}

// Delete removes a backup record by primary key ID.
func (r *BackupRepository) Delete(id uint) error {
	return r.db.Delete(&model.SystemBackup{}, id).Error
}
