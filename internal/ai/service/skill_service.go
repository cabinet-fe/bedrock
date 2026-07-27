package service

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/repository"
	rbacmodel "bedrock/internal/rbac/model"
	storagemodel "bedrock/internal/storage/model"
	storageservice "bedrock/internal/storage/service"
)

const (
	maxSkillZIPEntries       = 5000
	maxSkillUncompressed     = 200 * 1024 * 1024
	maxSkillCompressionRatio = 100
)

var (
	ErrMissingSkillMD = errors.New("ZIP 必须包含 SKILL.md")
	ErrSkillForbidden = errors.New("无权访问该 Skill")
	ErrSkillNotFound  = errors.New("技能不存在")
)

type SkillService struct {
	repo    *repository.AIRepository
	storage *storageservice.StorageService
	audit   AuditWriter
}

func NewSkillService(repo *repository.AIRepository, storage *storageservice.StorageService, audit ...AuditWriter) *SkillService {
	svc := &SkillService{repo: repo, storage: storage}
	if len(audit) > 0 {
		svc.audit = audit[0]
	}
	return svc
}

type SkillUploadInput struct {
	Name         string
	Description  string
	Visibility   string
	Filename     string
	ContentType  string
	Size         int64
	Source       io.Reader
	UserID       uint
	IsSuperAdmin bool
}

func (s *SkillService) List(page, pageSize int, userID uint, isSuperAdmin bool, dataScope string) ([]model.SkillPackage, int64, error) {
	return s.repo.ListSkills(page, pageSize, userID, isSuperAdmin, dataScope == rbacmodel.DataScopeAll)
}

func (s *SkillService) Get(id, userID uint, isSuperAdmin bool, dataScope string) (*model.SkillPackage, error) {
	skill, err := s.repo.FindSkill(id)
	if err != nil {
		return nil, ErrSkillNotFound
	}
	if !canViewSkill(skill, userID, isSuperAdmin, dataScope) {
		return nil, ErrSkillForbidden
	}
	return skill, nil
}

func (s *SkillService) Create(in SkillUploadInput) (*model.SkillPackage, error) {
	if err := validateVisibility(in.Visibility); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = strings.TrimSuffix(path.Base(in.Filename), path.Ext(in.Filename))
	}
	if name == "" {
		return nil, errors.New("名称不能为空")
	}
	object, err := s.putValidatedZIP(in)
	if err != nil {
		return nil, err
	}
	skill := &model.SkillPackage{
		Name: name, Description: strings.TrimSpace(in.Description),
		Visibility: in.Visibility, StorageObjectID: object.ID,
		PackageDigest: object.SHA256, SizeBytes: object.Size,
		CreatedBy: in.UserID, UpdatedBy: in.UserID,
	}
	if err := s.repo.CreateSkill(skill); err != nil {
		_ = s.storage.Delete(object.ID)
		return nil, err
	}
	if s.audit != nil {
		_ = s.audit.Write(in.UserID, "", "skill_create", "skill_package", fmt.Sprintf("%d", skill.ID), skill.Name, "")
	}
	return skill, nil
}

func (s *SkillService) Overwrite(id uint, in SkillUploadInput) (*model.SkillPackage, error) {
	skill, err := s.repo.FindSkill(id)
	if err != nil {
		return nil, ErrSkillNotFound
	}
	if skill.CreatedBy != in.UserID && !in.IsSuperAdmin {
		return nil, ErrSkillForbidden
	}
	if in.Visibility != "" {
		if err := validateVisibility(in.Visibility); err != nil {
			return nil, err
		}
		skill.Visibility = in.Visibility
	}
	if strings.TrimSpace(in.Name) != "" {
		skill.Name = strings.TrimSpace(in.Name)
	}
	if in.Description != "" {
		skill.Description = strings.TrimSpace(in.Description)
	}
	object, err := s.putValidatedZIP(in)
	if err != nil {
		return nil, err
	}
	oldID := skill.StorageObjectID
	skill.StorageObjectID = object.ID
	skill.PackageDigest = object.SHA256
	skill.SizeBytes = object.Size
	skill.UpdatedBy = in.UserID
	if err := s.repo.UpdateSkill(skill); err != nil {
		_ = s.storage.Delete(object.ID)
		return nil, err
	}
	_ = s.storage.Delete(oldID)
	if s.audit != nil {
		_ = s.audit.Write(in.UserID, "", "skill_overwrite", "skill_package", fmt.Sprintf("%d", skill.ID), skill.PackageDigest, "")
	}
	return skill, nil
}

func (s *SkillService) Delete(id, userID uint, isSuperAdmin bool) error {
	skill, err := s.repo.FindSkill(id)
	if err != nil {
		return ErrSkillNotFound
	}
	if skill.CreatedBy != userID && !isSuperAdmin {
		return ErrSkillForbidden
	}
	if err := s.repo.DeleteSkill(id); err != nil {
		return err
	}
	_ = s.storage.Delete(skill.StorageObjectID)
	return nil
}

func (s *SkillService) OpenPackage(id, userID uint, isSuperAdmin bool, dataScope string) (*model.SkillPackage, io.ReadCloser, string, error) {
	skill, err := s.Get(id, userID, isSuperAdmin, dataScope)
	if err != nil {
		return nil, nil, "", err
	}
	f, _, err := s.storage.Open(skill.StorageObjectID)
	if err != nil {
		return nil, nil, "", err
	}
	return skill, f, skill.Name + ".zip", nil
}

// InjectSkills extracts bound skills into workspaceDir/.agents/skills/<name>/ for agent runs.
// ZIP contents are rooted at the directory that contains SKILL.md (wrapping folders and
// __MACOSX are stripped), matching the Agent Skills layout.
func (s *SkillService) InjectSkills(workspaceDir string, skillIDs []uint, userID uint, isSuperAdmin bool) (map[uint]string, error) {
	digests := map[uint]string{}
	if len(skillIDs) == 0 {
		return digests, nil
	}
	skillsRoot := filepath.Join(workspaceDir, ".agents", "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		return nil, err
	}
	for _, id := range skillIDs {
		skill, err := s.repo.FindSkill(id)
		if err != nil {
			return nil, fmt.Errorf("skill %d: %w", id, ErrSkillNotFound)
		}
		if !canViewSkill(skill, userID, isSuperAdmin, rbacmodel.DataScopeSelf) {
			return nil, fmt.Errorf("skill %d: %w", id, ErrSkillForbidden)
		}
		f, obj, err := s.storage.Open(skill.StorageObjectID)
		if err != nil {
			return nil, err
		}
		dest := filepath.Join(skillsRoot, skillDirName(skill.Name))
		if err := extractSkillZIP(f, obj.Size, dest); err != nil {
			f.Close()
			return nil, err
		}
		f.Close()
		digests[id] = skill.PackageDigest
	}
	return digests, nil
}

func (s *SkillService) putValidatedZIP(in SkillUploadInput) (*storagemodel.StorageObject, error) {
	if !strings.EqualFold(path.Ext(in.Filename), ".zip") {
		return nil, errors.New("仅支持上传 .zip 文件")
	}
	object, err := s.storage.Put(storagemodel.KindSkillZIP, in.ContentType, in.Source, in.Size, in.UserID)
	if err != nil {
		return nil, err
	}
	f, obj, err := s.storage.Open(object.ID)
	if err != nil {
		_ = s.storage.Delete(object.ID)
		return nil, err
	}
	defer f.Close()
	if err := validateSkillZIP(f, obj.Size); err != nil {
		_ = s.storage.Delete(object.ID)
		return nil, err
	}
	return object, nil
}

func validateSkillZIP(r io.ReaderAt, size int64) error {
	reader, err := zip.NewReader(r, size)
	if err != nil {
		return errors.New("无效 ZIP 文件")
	}
	if len(reader.File) > maxSkillZIPEntries {
		return errors.New("ZIP 条目数超过限制")
	}
	var totalUncompressed uint64
	hasSkillMD := false
	for _, entry := range reader.File {
		if err := validateZIPEntry(entry); err != nil {
			return err
		}
		totalUncompressed += entry.UncompressedSize64
		if totalUncompressed > maxSkillUncompressed {
			return errors.New("ZIP 解压后内容超过限制")
		}
		if entry.CompressedSize64 > 0 {
			ratio := float64(entry.UncompressedSize64) / float64(entry.CompressedSize64)
			if ratio > maxSkillCompressionRatio && entry.UncompressedSize64 > 1024*1024 {
				return errors.New("ZIP 压缩比异常（疑似 zip bomb）")
			}
		}
		clean, err := cleanZIPPath(entry.Name)
		if err != nil {
			return err
		}
		if isJunkZIPPath(clean) {
			continue
		}
		if strings.EqualFold(path.Base(clean), "SKILL.md") {
			hasSkillMD = true
		}
	}
	if !hasSkillMD {
		return ErrMissingSkillMD
	}
	return nil
}

func extractSkillZIP(r io.ReaderAt, size int64, dest string) error {
	reader, err := zip.NewReader(r, size)
	if err != nil {
		return err
	}
	prefix, err := skillZIPContentPrefix(reader)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	cleanDest := filepath.Clean(dest)
	for _, entry := range reader.File {
		if err := validateZIPEntry(entry); err != nil {
			return err
		}
		clean, err := cleanZIPPath(entry.Name)
		if err != nil {
			return err
		}
		if isJunkZIPPath(clean) {
			continue
		}
		rel, ok := trimZIPPrefix(clean, prefix)
		if !ok {
			continue
		}
		if rel == "" {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if !strings.HasPrefix(target, cleanDest+string(os.PathSeparator)) && target != cleanDest {
			return errors.New("非法 ZIP 路径")
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := entry.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(rc, maxSkillUncompressed))
		rc.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

// skillZIPContentPrefix returns the directory prefix that contains SKILL.md so a ZIP
// packaged as java-api-docs/SKILL.md extracts as SKILL.md under the dest folder.
func skillZIPContentPrefix(reader *zip.Reader) (string, error) {
	var skillMD string
	for _, entry := range reader.File {
		clean, err := cleanZIPPath(entry.Name)
		if err != nil {
			return "", err
		}
		if isJunkZIPPath(clean) {
			continue
		}
		if strings.EqualFold(path.Base(clean), "SKILL.md") {
			skillMD = clean
			break
		}
	}
	if skillMD == "" {
		return "", ErrMissingSkillMD
	}
	dir := path.Dir(skillMD)
	if dir == "." || dir == "" {
		return "", nil
	}
	return dir + "/", nil
}

func trimZIPPrefix(clean, prefix string) (string, bool) {
	if prefix == "" {
		return clean, true
	}
	if clean == strings.TrimSuffix(prefix, "/") {
		return "", true
	}
	if !strings.HasPrefix(clean, prefix) {
		return "", false
	}
	return strings.TrimPrefix(clean, prefix), true
}

func isJunkZIPPath(clean string) bool {
	if clean == "__MACOSX" || strings.HasPrefix(clean, "__MACOSX/") {
		return true
	}
	base := path.Base(clean)
	return strings.HasPrefix(base, "._")
}

func skillDirName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "\\", "-")
	name = strings.Trim(name, ". ")
	if name == "" || name == "." || name == ".." {
		return "skill"
	}
	return name
}

func validateZIPEntry(entry *zip.File) error {
	if entry.UncompressedSize64 > maxSkillUncompressed {
		return errors.New("ZIP 单文件过大")
	}
	name := entry.Name
	if !utf8.ValidString(name) {
		return errors.New("ZIP 条目名非法")
	}
	if strings.Contains(name, "..") {
		return errors.New("非法 ZIP 路径（Zip Slip）")
	}
	return nil
}

func cleanZIPPath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Clean("/" + name)
	name = strings.TrimPrefix(name, "/")
	if name == "" || name == "." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
		return "", errors.New("非法 ZIP 路径（Zip Slip）")
	}
	return name, nil
}

func validateVisibility(v string) error {
	switch v {
	case model.SkillPublic, model.SkillPrivate:
		return nil
	default:
		return errors.New("visibility 必须为 public 或 private")
	}
}

func canViewSkill(skill *model.SkillPackage, userID uint, isSuperAdmin bool, dataScope string) bool {
	if isSuperAdmin || dataScope == rbacmodel.DataScopeAll {
		return true
	}
	if skill.Visibility == model.SkillPublic {
		return true
	}
	return skill.CreatedBy == userID
}
