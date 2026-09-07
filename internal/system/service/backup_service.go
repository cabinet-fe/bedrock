package service

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	authmodel "bedrock/internal/auth/model"
	"bedrock/internal/pkg"
	"bedrock/internal/system/model"
	"bedrock/internal/system/repository"
)

var (
	ErrBackupNotFound       = errors.New("备份记录不存在")
	ErrInvalidAdminPassword = errors.New("管理员密码错误")
	ErrInvalidModules       = errors.New("请选择至少一个有效的备份模块")
	ErrIncompatibleBackup   = errors.New("备份包与当前系统环境不兼容")
)

// UserFinder defines the interface to look up a user by ID for password validation.
type UserFinder interface {
	FindByID(id uint) (*authmodel.User, error)
}

// BackupService coordinates backup creation, list queries, downloads,
// deletion of records and files, and administrative restoration.
type BackupService struct {
	repo   *repository.BackupRepository
	engine *BackupEngine
	users  UserFinder
}

// NewBackupService constructs a new BackupService.
func NewBackupService(repo *repository.BackupRepository, engine *BackupEngine, users UserFinder) *BackupService {
	return &BackupService{
		repo:   repo,
		engine: engine,
		users:  users,
	}
}

// List queries backup records with pagination.
func (s *BackupService) List(q pkg.ListQuery) ([]model.SystemBackup, int64, error) {
	return s.repo.List(q)
}

// CreateBackup orchestrates creation of a system backup archive.
func (s *BackupService) CreateBackup(userID uint, req model.SystemBackupCreateRequest) (*model.SystemBackup, error) {
	if len(req.Modules) == 0 {
		return nil, ErrInvalidModules
	}

	seen := make(map[string]bool)
	var cleanModules []string
	for _, m := range req.Modules {
		m = strings.TrimSpace(m)
		if !model.IsValidBackupModule(m) {
			return nil, fmt.Errorf("不支持的备份模块: %s", m)
		}
		if !seen[m] {
			seen[m] = true
			cleanModules = append(cleanModules, m)
		}
	}
	if len(cleanModules) == 0 {
		return nil, ErrInvalidModules
	}

	if err := os.MkdirAll(s.engine.BackupDir(), 0o755); err != nil {
		return nil, fmt.Errorf("创建备份目录失败: %w", err)
	}

	filename := fmt.Sprintf("backup_%s_%04d.zip", time.Now().UTC().Format("20060102_150405"), rand.Intn(10000))
	destPath := filepath.Join(s.engine.BackupDir(), filename)

	backup := &model.SystemBackup{
		Filename:  filename,
		FilePath:  destPath,
		FileSize:  0,
		Modules:   cleanModules,
		Note:      strings.TrimSpace(req.Note),
		Status:    model.BackupStatusProcessing,
		CreatedBy: userID,
	}

	if err := s.repo.Create(backup); err != nil {
		return nil, fmt.Errorf("创建备份记录失败: %w", err)
	}

	_, fileSize, err := s.engine.Pack(destPath, cleanModules, backup.Note)
	if err != nil {
		_ = s.repo.UpdateFailed(backup.ID, err.Error())
		_ = os.Remove(destPath)
		backup.Status = model.BackupStatusFailed
		backup.ErrorMessage = err.Error()
		return nil, fmt.Errorf("打包备份失败: %w", err)
	}

	if err := s.repo.UpdateSuccess(backup.ID, fileSize); err != nil {
		return nil, fmt.Errorf("更新备份状态失败: %w", err)
	}

	backup.Status = model.BackupStatusSuccess
	backup.FileSize = fileSize
	backup.ErrorMessage = ""
	return backup, nil
}

// GetDownloadPath retrieves the physical file path and filename for downloading a backup.
func (s *BackupService) GetDownloadPath(id uint) (string, string, error) {
	b, err := s.repo.FindByID(id)
	if err != nil {
		return "", "", ErrBackupNotFound
	}
	if b.Status != model.BackupStatusSuccess {
		return "", "", errors.New("备份未成功，无法下载")
	}
	if _, err := os.Stat(b.FilePath); err != nil {
		if os.IsNotExist(err) {
			return "", "", errors.New("备份物理文件不存在")
		}
		return "", "", err
	}
	return b.FilePath, b.Filename, nil
}

// DeleteBackup deletes a backup record and its associated physical zip file.
func (s *BackupService) DeleteBackup(id uint) error {
	b, err := s.repo.FindByID(id)
	if err != nil {
		return ErrBackupNotFound
	}

	if b.FilePath != "" {
		_ = os.Remove(b.FilePath)
	}

	return s.repo.Delete(id)
}

// InspectUploadedBackup saves an uploaded backup zip to a temporary file and inspects its manifest.
func (s *BackupService) InspectUploadedBackup(filename string, r io.Reader, size int64) (*model.BackupInspectResult, error) {
	if !strings.HasSuffix(strings.ToLower(filename), ".zip") {
		return nil, errors.New("仅支持 .zip 格式的备份文件")
	}

	uploadsDir := filepath.Join(s.engine.BackupDir(), "uploads")
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建上传目录失败: %w", err)
	}

	token := fmt.Sprintf("ut_%d_%06d", time.Now().UnixNano(), rand.Intn(1000000))
	tmpPath := filepath.Join(uploadsDir, token+".zip")

	out, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, r); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("保存上传文件失败: %w", err)
	}

	return s.engine.InspectPackage(tmpPath, token)
}

// Restore validates the admin password and restores data from an existing backup or uploaded token.
func (s *BackupService) Restore(currentUserID uint, req model.SystemBackupRestoreRequest) (*model.SystemBackupRestoreResponse, error) {
	if strings.TrimSpace(req.AdminPassword) == "" {
		return nil, errors.New("请输入管理员密码")
	}

	hasBackupID := req.BackupID != nil && *req.BackupID > 0
	hasToken := strings.TrimSpace(req.UploadToken) != ""
	if !hasBackupID && !hasToken {
		return nil, errors.New("请指定备份记录 ID 或上传凭证")
	}
	if hasBackupID && hasToken {
		return nil, errors.New("不能同时指定备份记录 ID 与上传凭证")
	}

	if currentUserID == 0 {
		return nil, errors.New("未授权用户")
	}
	user, err := s.users.FindByID(currentUserID)
	if err != nil || user == nil {
		return nil, errors.New("当前用户不存在")
	}
	if !pkg.CheckPassword(req.AdminPassword, user.PasswordHash) {
		return nil, ErrInvalidAdminPassword
	}

	var targetPath string
	var isUpload bool
	if hasBackupID {
		b, err := s.repo.FindByID(*req.BackupID)
		if err != nil {
			return nil, ErrBackupNotFound
		}
		if b.Status != model.BackupStatusSuccess {
			return nil, errors.New("备份记录状态非成功，无法用于恢复")
		}
		if _, err := os.Stat(b.FilePath); err != nil {
			if os.IsNotExist(err) {
				return nil, errors.New("备份物理文件不存在")
			}
			return nil, err
		}
		targetPath = b.FilePath
	} else {
		isUpload = true
		token := filepath.Base(strings.TrimSpace(req.UploadToken))
		token = strings.TrimSuffix(token, ".zip")
		uploadPath := filepath.Join(s.engine.BackupDir(), "uploads", token+".zip")
		if _, err := os.Stat(uploadPath); err != nil {
			if os.IsNotExist(err) {
				return nil, errors.New("上传的备份文件不存在或已失效")
			}
			return nil, err
		}
		targetPath = uploadPath
	}

	manifest, err := s.engine.Inspect(targetPath)
	if err != nil {
		return nil, fmt.Errorf("备份包解析失败: %w", err)
	}
	compatible, reason := s.engine.CheckCompatibility(manifest)
	if !compatible {
		return nil, fmt.Errorf("%w: %s", ErrIncompatibleBackup, reason)
	}

	var snapshotPath, snapshotFilename string
	var snapshotSize int64
	if req.ShouldAutoSnapshot() {
		var snapErr error
		snapshotPath, snapshotFilename, _, snapshotSize, snapErr = s.engine.CreateAutoSnapshot()
		if snapErr != nil {
			return nil, fmt.Errorf("创建还原前应急快照失败: %w", snapErr)
		}
		_ = s.repo.Create(&model.SystemBackup{
			Filename:  snapshotFilename,
			FilePath:  snapshotPath,
			FileSize:  snapshotSize,
			Modules:   model.AllBackupModules,
			Note:      "还原前自动快照",
			Status:    model.BackupStatusSuccess,
			CreatedBy: currentUserID,
		})
	}

	if err := s.engine.Restore(targetPath); err != nil {
		return nil, fmt.Errorf("恢复系统数据失败: %w", err)
	}

	if isUpload {
		_ = os.Remove(targetPath)
	}

	if snapshotPath != "" {
		if _, err := s.repo.FindByFilename(snapshotFilename); err != nil {
			_ = s.repo.Create(&model.SystemBackup{
				Filename:  snapshotFilename,
				FilePath:  snapshotPath,
				FileSize:  snapshotSize,
				Modules:   model.AllBackupModules,
				Note:      "还原前自动快照",
				Status:    model.BackupStatusSuccess,
				CreatedBy: currentUserID,
			})
		}
	}

	return &model.SystemBackupRestoreResponse{
		Success: true,
		Message: "系统恢复成功，请刷新页面",
	}, nil
}
