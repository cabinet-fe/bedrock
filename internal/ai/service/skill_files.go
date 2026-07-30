package service

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"bedrock/internal/ai/model"
	storagemodel "bedrock/internal/storage/model"
)

const (
	maxSkillEditBytes = 2 * 1024 * 1024
	skillMDName       = "SKILL.md"
)

// SkillFileNode is one entry in the skill working-copy tree.
type SkillFileNode struct {
	Name     string          `json:"name"`
	Path     string          `json:"path"`
	Kind     string          `json:"kind"` // file | dir
	Size     int64           `json:"size,omitempty"`
	Children []SkillFileNode `json:"children,omitempty"`
}

// SkillFileContent is the API projection of a readable text file.
type SkillFileContent struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Size     int64  `json:"size"`
	Binary   bool   `json:"binary"`
	Editable bool   `json:"editable"`
}

type SkillCreateEntryInput struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"` // file | dir
	Content string `json:"content"`
}

type SkillWriteFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type SkillRenameInput struct {
	FromPath string `json:"from_path"`
	ToPath   string `json:"to_path"`
}

func (s *SkillService) ListFiles(id, userID uint, isSuperAdmin bool, dataScope string) ([]SkillFileNode, error) {
	_, root, err := s.openSkillRoot(id, userID, isSuperAdmin, dataScope, false)
	if err != nil {
		return nil, err
	}
	return buildSkillTree(root, "")
}

func (s *SkillService) ReadFile(id, userID uint, isSuperAdmin bool, dataScope, relPath string) (*SkillFileContent, error) {
	skill, root, err := s.openSkillRoot(id, userID, isSuperAdmin, dataScope, false)
	if err != nil {
		return nil, err
	}
	clean, abs, err := s.resolveSkillPath(root, relPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrSkillFileNotFound
		}
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("不能读取目录内容")
	}
	if info.Size() > maxSkillEditBytes {
		return nil, ErrSkillFileTooLarge
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	out := &SkillFileContent{
		Path:     clean,
		Size:     info.Size(),
		Editable: canEditSkill(skill, userID, isSuperAdmin),
	}
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		out.Binary = true
		return out, nil
	}
	out.Content = string(raw)
	return out, nil
}

func (s *SkillService) WriteFile(id, userID uint, isSuperAdmin bool, dataScope string, in SkillWriteFileInput) (*SkillFileContent, error) {
	skill, root, err := s.openSkillRoot(id, userID, isSuperAdmin, dataScope, true)
	if err != nil {
		return nil, err
	}
	clean, abs, err := s.resolveSkillPath(root, in.Path)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return nil, fmt.Errorf("目标是目录，不能写入文件内容")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if int64(len(in.Content)) > maxSkillEditBytes {
		return nil, ErrSkillFileTooLarge
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(abs, []byte(in.Content), 0o644); err != nil {
		return nil, err
	}
	if err := s.afterWorkingCopyMutation(skill, userID); err != nil {
		return nil, err
	}
	return &SkillFileContent{
		Path: clean, Content: in.Content, Size: int64(len(in.Content)), Editable: true,
	}, nil
}

func (s *SkillService) CreateEntry(id, userID uint, isSuperAdmin bool, dataScope string, in SkillCreateEntryInput) (*SkillFileNode, error) {
	skill, root, err := s.openSkillRoot(id, userID, isSuperAdmin, dataScope, true)
	if err != nil {
		return nil, err
	}
	clean, abs, err := s.resolveSkillPath(root, in.Path)
	if err != nil {
		return nil, err
	}
	if clean == "" || clean == "." {
		return nil, fmt.Errorf("路径不能为空")
	}
	if _, err := os.Stat(abs); err == nil {
		return nil, ErrSkillFileExists
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	kind := strings.TrimSpace(in.Kind)
	switch kind {
	case "dir":
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return nil, err
		}
	case "file":
		if int64(len(in.Content)) > maxSkillEditBytes {
			return nil, ErrSkillFileTooLarge
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(abs, []byte(in.Content), 0o644); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("kind 必须为 file 或 dir")
	}
	if err := s.afterWorkingCopyMutation(skill, userID); err != nil {
		return nil, err
	}
	node := &SkillFileNode{Name: path.Base(clean), Path: clean, Kind: kind}
	if kind == "file" {
		node.Size = int64(len(in.Content))
	}
	return node, nil
}

func (s *SkillService) DeleteEntry(id, userID uint, isSuperAdmin bool, dataScope, relPath string) error {
	skill, root, err := s.openSkillRoot(id, userID, isSuperAdmin, dataScope, true)
	if err != nil {
		return err
	}
	clean, abs, err := s.resolveSkillPath(root, relPath)
	if err != nil {
		return err
	}
	if clean == "" || clean == "." {
		return fmt.Errorf("不能删除技能根目录")
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrSkillFileNotFound
		}
		return err
	}
	if strings.EqualFold(path.Base(clean), skillMDName) {
		return fmt.Errorf("不能删除 %s", skillMDName)
	}
	if info.IsDir() {
		if containsSkillMD(abs) {
			return fmt.Errorf("目录内包含 %s，不能删除", skillMDName)
		}
	}
	if err := os.RemoveAll(abs); err != nil {
		return err
	}
	return s.afterWorkingCopyMutation(skill, userID)
}

func (s *SkillService) RenameEntry(id, userID uint, isSuperAdmin bool, dataScope string, in SkillRenameInput) (*SkillFileNode, error) {
	skill, root, err := s.openSkillRoot(id, userID, isSuperAdmin, dataScope, true)
	if err != nil {
		return nil, err
	}
	fromClean, fromAbs, err := s.resolveSkillPath(root, in.FromPath)
	if err != nil {
		return nil, err
	}
	toClean, toAbs, err := s.resolveSkillPath(root, in.ToPath)
	if err != nil {
		return nil, err
	}
	if fromClean == "" || fromClean == "." {
		return nil, fmt.Errorf("不能重命名技能根目录")
	}
	info, err := os.Stat(fromAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrSkillFileNotFound
		}
		return nil, err
	}
	if _, err := os.Stat(toAbs); err == nil {
		return nil, ErrSkillFileExists
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	fromIsSkillMD := strings.EqualFold(path.Base(fromClean), skillMDName)
	toIsSkillMD := strings.EqualFold(path.Base(toClean), skillMDName)
	if fromIsSkillMD && !toIsSkillMD {
		return nil, fmt.Errorf("不能将 %s 重命名为其他名称", skillMDName)
	}
	if err := os.MkdirAll(filepath.Dir(toAbs), 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(fromAbs, toAbs); err != nil {
		return nil, err
	}
	if err := ensureSkillMDPresent(root); err != nil {
		_ = os.Rename(toAbs, fromAbs)
		return nil, err
	}
	if err := s.afterWorkingCopyMutation(skill, userID); err != nil {
		return nil, err
	}
	kind := "file"
	var size int64
	if info.IsDir() {
		kind = "dir"
	} else {
		size = info.Size()
	}
	return &SkillFileNode{Name: path.Base(toClean), Path: toClean, Kind: kind, Size: size}, nil
}

func (s *SkillService) openSkillRoot(id, userID uint, isSuperAdmin bool, dataScope string, forWrite bool) (*model.SkillPackage, string, error) {
	skill, err := s.Get(id, userID, isSuperAdmin, dataScope)
	if err != nil {
		return nil, "", err
	}
	if forWrite {
		if !canEditSkill(skill, userID, isSuperAdmin) {
			if skill.Source == model.SkillSourceBuiltin {
				return nil, "", ErrSkillReadOnly
			}
			return nil, "", ErrSkillForbidden
		}
	}
	root, err := s.ensureWorkingCopy(skill)
	if err != nil {
		return nil, "", err
	}
	return skill, root, nil
}

func (s *SkillService) skillDir(id uint) string {
	return filepath.Join(s.skillsRoot, fmt.Sprintf("%d", id))
}

func (s *SkillService) removeWorkingCopy(id uint) error {
	if strings.TrimSpace(s.skillsRoot) == "" {
		return nil
	}
	return os.RemoveAll(s.skillDir(id))
}

func (s *SkillService) replaceWorkingCopy(skill *model.SkillPackage) error {
	if skill == nil {
		return fmt.Errorf("skill 为空")
	}
	if strings.TrimSpace(s.skillsRoot) == "" {
		return fmt.Errorf("skills 工作目录未配置")
	}
	dir := s.skillDir(skill.ID)
	tmp := dir + ".tmp"
	_ = os.RemoveAll(tmp)
	_ = os.RemoveAll(dir)
	f, obj, err := s.storage.Open(skill.StorageObjectID)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := extractSkillZIP(f, obj.Size, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, dir); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	return nil
}

func (s *SkillService) ensureWorkingCopy(skill *model.SkillPackage) (string, error) {
	if skill == nil {
		return "", fmt.Errorf("skill 为空")
	}
	if strings.TrimSpace(s.skillsRoot) == "" {
		return "", fmt.Errorf("skills 工作目录未配置")
	}
	if err := os.MkdirAll(s.skillsRoot, 0o755); err != nil {
		return "", err
	}
	dir := s.skillDir(skill.ID)
	if err := ensureSkillMDPresent(dir); err == nil {
		return dir, nil
	}
	if err := s.replaceWorkingCopy(skill); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *SkillService) resolveSkillPath(root, rel string) (clean string, abs string, err error) {
	rel = strings.ReplaceAll(rel, "\\", "/")
	rel = path.Clean("/" + strings.TrimSpace(rel))
	rel = strings.TrimPrefix(rel, "/")
	if rel == "." {
		rel = ""
	}
	if strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") || rel == ".." {
		return "", "", ErrSkillPathInvalid
	}
	cleanRoot := filepath.Clean(root)
	abs = cleanRoot
	if rel != "" {
		abs = filepath.Join(cleanRoot, filepath.FromSlash(rel))
	}
	relToRoot, err := filepath.Rel(cleanRoot, abs)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(os.PathSeparator)) {
		return "", "", ErrSkillPathInvalid
	}
	return rel, abs, nil
}

func (s *SkillService) afterWorkingCopyMutation(skill *model.SkillPackage, userID uint) error {
	if err := ensureSkillMDPresent(s.skillDir(skill.ID)); err != nil {
		return err
	}
	object, err := s.packWorkingCopy(skill.ID, userID)
	if err != nil {
		return err
	}
	oldID := skill.StorageObjectID
	skill.StorageObjectID = object.ID
	skill.PackageDigest = object.SHA256
	skill.SizeBytes = object.Size
	skill.UpdatedBy = userID
	if err := s.repo.UpdateSkill(skill); err != nil {
		_ = s.storage.Delete(object.ID)
		return err
	}
	if oldID != 0 && oldID != object.ID {
		_ = s.storage.Delete(oldID)
	}
	if s.audit != nil {
		_ = s.audit.Write(userID, "", "skill_files_update", "skill_package", fmt.Sprintf("%d", skill.ID), skill.PackageDigest, "")
	}
	return nil
}

func (s *SkillService) packWorkingCopy(id, userID uint) (*storagemodel.StorageObject, error) {
	root := s.skillDir(id)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := filepath.ToSlash(rel)
		if d.IsDir() {
			_, err := zw.Create(name + "/")
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxSkillUncompressed {
			return fmt.Errorf("打包时单文件过大")
		}
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, io.LimitReader(f, maxSkillUncompressed))
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	data := buf.Bytes()
	return s.storage.Put(storagemodel.KindSkillZIP, "application/zip", bytes.NewReader(data), int64(len(data)), userID)
}

func buildSkillTree(root, prefix string) ([]SkillFileNode, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	nodes := make([]SkillFileNode, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		rel := name
		if prefix != "" {
			rel = path.Join(prefix, name)
		}
		node := SkillFileNode{Name: name, Path: rel}
		if entry.IsDir() {
			node.Kind = "dir"
			children, err := buildSkillTree(filepath.Join(root, name), rel)
			if err != nil {
				return nil, err
			}
			node.Children = children
		} else {
			node.Kind = "file"
			if info, err := entry.Info(); err == nil {
				node.Size = info.Size()
			}
		}
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Kind != nodes[j].Kind {
			return nodes[i].Kind == "dir"
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
	return nodes, nil
}

func ensureSkillMDPresent(root string) error {
	if containsSkillMD(root) {
		return nil
	}
	return ErrMissingSkillMD
}

func containsSkillMD(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.EqualFold(d.Name(), skillMDName) {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func copySkillDir(src, dest string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		in.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}
