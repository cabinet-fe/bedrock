package service

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	projectmodel "bedrock/internal/project/model"
	storagemodel "bedrock/internal/storage/model"

	"gorm.io/gorm"
)

type DevDocNodeInput struct {
	ParentID     *uint   `json:"parent_id"`
	Kind         string  `json:"kind"`
	Name         string  `json:"name"`
	SortOrder    int     `json:"sort_order"`
	RepositoryID *uint   `json:"repository_id"`
	Content      *string `json:"content"`
}

type DevDocMoveInput struct {
	ParentID  *uint `json:"parent_id"`
	SortOrder int   `json:"sort_order"`
}

func (s *ProjectService) ListDevDocTree(actor AccessContext, projectID uint) ([]projectmodel.DevDocNode, error) {
	if _, err := s.acl.Require(projectID, actor, "project_dev_docs:view", capDevDocView); err != nil {
		return nil, err
	}
	nodes, err := s.repo.ListDevDocTreeNodes(projectID)
	if err != nil {
		return nil, err
	}
	return buildDevDocTree(nodes), nil
}

func (s *ProjectService) GetDevDocNode(actor AccessContext, id uint) (*projectmodel.DevDocNode, error) {
	node, err := s.repo.FindDevDocNode(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("文档节点不存在")
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.acl.Require(node.ProjectID, actor, "project_dev_docs:view", capDevDocView); err != nil {
		return nil, err
	}
	return node, nil
}

// CheckDevDocProject validates nested document routes without imposing a separate
// global :view grant on callers that only hold a write permission.
func (s *ProjectService) CheckDevDocProject(actor AccessContext, projectID, nodeID uint, globalPermission string, write bool) error {
	node, err := s.repo.FindDevDocNode(nodeID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewNotFound("文档节点不存在")
	}
	if err != nil {
		return err
	}
	if node.ProjectID != projectID {
		return NewNotFound("文档节点不存在")
	}
	capability := capDevDocView
	if write {
		capability = capDevDocEdit
	}
	_, err = s.acl.Require(projectID, actor, globalPermission, capability)
	return err
}

func (s *ProjectService) CreateDevDocNode(actor AccessContext, projectID uint, input DevDocNodeInput) (*projectmodel.DevDocNode, error) {
	if _, err := s.acl.Require(projectID, actor, "project_dev_docs:create", capDevDocEdit); err != nil {
		return nil, err
	}
	if err := s.requireActiveProject(projectID); err != nil {
		return nil, err
	}
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	if kind != projectmodel.DocNodeDirectory && kind != projectmodel.DocNodeDocument {
		return nil, errors.New("节点类型必须为 dir 或 doc")
	}
	name := safeDocName(input.Name)
	if name == "" {
		return nil, errors.New("文档节点名称不能为空")
	}
	if err := s.validateDevDocParent(projectID, input.ParentID); err != nil {
		return nil, err
	}
	node := &projectmodel.DevDocNode{
		ProjectID: projectID, ParentID: input.ParentID, Kind: kind, Name: name, SortOrder: input.SortOrder,
		RepositoryID: input.RepositoryID, CreatedBy: actor.UserID, UpdatedBy: actor.UserID,
	}
	if kind == projectmodel.DocNodeDocument && input.Content != nil {
		node.Content = *input.Content
	}
	if err := s.repo.CreateDevDocNode(node); err != nil {
		return nil, err
	}
	return node, nil
}

func (s *ProjectService) UpdateDevDocNode(actor AccessContext, id uint, input DevDocNodeInput) (*projectmodel.DevDocNode, error) {
	node, err := s.repo.FindDevDocNode(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("文档节点不存在")
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.acl.Require(node.ProjectID, actor, "project_dev_docs:update", capDevDocEdit); err != nil {
		return nil, err
	}
	if err := s.requireActiveProject(node.ProjectID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Name) != "" {
		name := safeDocName(input.Name)
		if name == "" {
			return nil, errors.New("文档节点名称不能为空")
		}
		node.Name = name
	}
	if input.RepositoryID != nil {
		node.RepositoryID = input.RepositoryID
	}
	if input.Content != nil {
		if node.Kind != projectmodel.DocNodeDocument {
			return nil, errors.New("目录不能编辑 Markdown 内容")
		}
		s.writeDevContent(node, *input.Content, actor.UserID)
	}
	node.UpdatedBy = actor.UserID
	if err := s.repo.UpdateDevDocNode(node); err != nil {
		return nil, err
	}
	return node, nil
}

func (s *ProjectService) MoveDevDocNode(actor AccessContext, id uint, input DevDocMoveInput) (*projectmodel.DevDocNode, error) {
	node, err := s.repo.FindDevDocNode(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("文档节点不存在")
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.acl.Require(node.ProjectID, actor, "project_dev_docs:update", capDevDocEdit); err != nil {
		return nil, err
	}
	if err := s.requireActiveProject(node.ProjectID); err != nil {
		return nil, err
	}
	if err := s.validateDevDocParent(node.ProjectID, input.ParentID); err != nil {
		return nil, err
	}
	if input.ParentID != nil && *input.ParentID == node.ID {
		return nil, errors.New("节点不能移动到自身")
	}
	nodes, err := s.repo.ListDevDocNodes(node.ProjectID)
	if err != nil {
		return nil, err
	}
	descendants := devDocSubtreeIDs(nodes, node.ID)
	if input.ParentID != nil {
		if slices.Contains(descendants, *input.ParentID) {
			return nil, errors.New("节点不能移动到自己的子节点")
		}
	}
	node.ParentID = input.ParentID
	node.SortOrder = input.SortOrder
	node.UpdatedBy = actor.UserID
	if err := s.repo.UpdateDevDocNode(node); err != nil {
		return nil, err
	}
	return node, nil
}

func (s *ProjectService) DeleteDevDocNode(actor AccessContext, id uint) error {
	node, err := s.repo.FindDevDocNode(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewNotFound("文档节点不存在")
	}
	if err != nil {
		return err
	}
	if _, err := s.acl.Require(node.ProjectID, actor, "project_dev_docs:delete", capDevDocAdmin); err != nil {
		return err
	}
	nodes, err := s.repo.ListDevDocNodes(node.ProjectID)
	if err != nil {
		return err
	}
	return s.repo.DeleteDevDocNodes(devDocSubtreeIDs(nodes, id))
}

func (s *ProjectService) UploadDevMarkdown(actor AccessContext, projectID uint, parentID *uint, filename, contentType string, source io.Reader, size int64) (*projectmodel.DevDocNode, error) {
	if _, err := s.acl.Require(projectID, actor, "project_dev_docs:create", capDevDocEdit); err != nil {
		return nil, err
	}
	if err := s.requireActiveProject(projectID); err != nil {
		return nil, err
	}
	if !strings.EqualFold(path.Ext(filename), ".md") {
		return nil, errors.New("仅支持上传 .md 文件")
	}
	if err := s.validateDevDocParent(projectID, parentID); err != nil {
		return nil, err
	}
	object, err := s.storage.Put(storagemodel.KindDocImport, contentType, source, size, actor.UserID)
	if err != nil {
		return nil, err
	}
	defer s.storage.Delete(object.ID)
	file, _, err := s.storage.Open(object.ID)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return s.createImportedDevDocument(projectID, parentID, safeDocName(path.Base(filename)), string(content), actor.UserID)
}

func (s *ProjectService) ImportDevZIP(actor AccessContext, projectID uint, parentID *uint, filename, contentType string, source io.Reader, size int64) ([]projectmodel.DevDocNode, error) {
	if _, err := s.acl.Require(projectID, actor, "project_dev_docs:create", capDevDocEdit); err != nil {
		return nil, err
	}
	if err := s.requireActiveProject(projectID); err != nil {
		return nil, err
	}
	if !strings.EqualFold(path.Ext(filename), ".zip") {
		return nil, errors.New("仅支持上传 .zip 文件")
	}
	if err := s.validateDevDocParent(projectID, parentID); err != nil {
		return nil, err
	}
	object, err := s.storage.Put(storagemodel.KindDocImport, contentType, source, size, actor.UserID)
	if err != nil {
		return nil, err
	}
	defer s.storage.Delete(object.ID)
	file, _, err := s.storage.Open(object.ID)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader, err := zip.NewReader(file, object.Size)
	if err != nil {
		return nil, errors.New("无效 ZIP 文件")
	}
	if len(reader.File) > maxZIPEntries {
		return nil, errors.New("ZIP 条目数超过限制")
	}
	var totalUncompressed uint64
	for _, entry := range reader.File {
		if err := validateZIPEntry(entry); err != nil {
			return nil, err
		}
		totalUncompressed += entry.UncompressedSize64
		if totalUncompressed > uint64(s.storage.MaxBytes(storagemodel.KindDocImport)) {
			return nil, errors.New("ZIP 解压后内容超过限制")
		}
	}

	nodes, err := s.repo.ListDevDocNodes(projectID)
	if err != nil {
		return nil, err
	}
	index := newDevDocImportIndex(nodes, parentID)
	imported := make([]projectmodel.DevDocNode, 0)
	for _, entry := range reader.File {
		clean, err := cleanZIPPath(entry.Name)
		if err != nil {
			return nil, err
		}
		if entry.FileInfo().IsDir() || !strings.EqualFold(path.Ext(clean), ".md") {
			continue
		}
		parts := strings.Split(clean, "/")
		documentName := safeDocName(parts[len(parts)-1])
		currentParent := parentID
		currentKey := importDevParentPath(nodes, parentID)
		for _, directory := range parts[:len(parts)-1] {
			key := currentKey + "/" + directory
			if existing, ok := index[key]; ok {
				if existing.Kind != projectmodel.DocNodeDirectory {
					return nil, fmt.Errorf("导入路径与文档节点冲突: %s", directory)
				}
				id := existing.ID
				currentParent = &id
				currentKey = key
				continue
			}
			directoryNode := &projectmodel.DevDocNode{
				ProjectID: projectID, ParentID: currentParent, Kind: projectmodel.DocNodeDirectory, Name: directory,
				CreatedBy: actor.UserID, UpdatedBy: actor.UserID,
			}
			if err := s.repo.CreateDevDocNode(directoryNode); err != nil {
				return nil, err
			}
			index[key] = *directoryNode
			id := directoryNode.ID
			currentParent = &id
			currentKey = key
		}
		content, err := readZIPEntry(entry, s.storage.MaxBytes(storagemodel.KindDocImport))
		if err != nil {
			return nil, err
		}
		documentKey := currentKey + "/" + documentName
		if existing, ok := index[documentKey]; ok {
			if existing.Kind != projectmodel.DocNodeDocument {
				return nil, fmt.Errorf("导入路径与目录节点冲突: %s", documentName)
			}
			node := existing
			s.writeDevContent(&node, string(content), actor.UserID)
			if err := s.repo.UpdateDevDocNode(&node); err != nil {
				return nil, err
			}
			index[documentKey] = node
			imported = append(imported, node)
			continue
		}
		node, err := s.createImportedDevDocument(projectID, currentParent, documentName, string(content), actor.UserID)
		if err != nil {
			return nil, err
		}
		index[documentKey] = *node
		imported = append(imported, *node)
	}
	return imported, nil
}

// UpsertDevDocByPath creates missing directories under doc_dir and upserts a document by name.
// created is true when a new document node was inserted.
func (s *ProjectService) UpsertDevDocByPath(actor AccessContext, projectID uint, docDir, docName, content string) (*projectmodel.DevDocNode, bool, error) {
	if _, err := s.acl.Require(projectID, actor, "project_dev_docs:create", capDevDocEdit); err != nil {
		return nil, false, err
	}
	if err := s.requireActiveProject(projectID); err != nil {
		return nil, false, err
	}
	dirs, err := parseRelDirPath(docDir, "doc_dir")
	if err != nil {
		return nil, false, err
	}
	docName, err = normalizeMDFileName(docName, "doc_name")
	if err != nil {
		return nil, false, err
	}

	nodes, err := s.repo.ListDevDocNodes(projectID)
	if err != nil {
		return nil, false, err
	}
	index := newDevDocImportIndex(nodes, nil)
	currentParent := (*uint)(nil)
	currentKey := "root"
	for _, directory := range dirs {
		key := currentKey + "/" + directory
		if existing, ok := index[key]; ok {
			if existing.Kind != projectmodel.DocNodeDirectory {
				return nil, false, fmt.Errorf("路径与文档节点冲突: %s", directory)
			}
			id := existing.ID
			currentParent = &id
			currentKey = key
			continue
		}
		directoryNode := &projectmodel.DevDocNode{
			ProjectID: projectID, ParentID: currentParent, Kind: projectmodel.DocNodeDirectory, Name: directory,
			CreatedBy: actor.UserID, UpdatedBy: actor.UserID,
		}
		if err := s.repo.CreateDevDocNode(directoryNode); err != nil {
			return nil, false, err
		}
		index[key] = *directoryNode
		id := directoryNode.ID
		currentParent = &id
		currentKey = key
	}

	documentKey := currentKey + "/" + docName
	if existing, ok := index[documentKey]; ok {
		if existing.Kind != projectmodel.DocNodeDocument {
			return nil, false, fmt.Errorf("路径与目录节点冲突: %s", docName)
		}
		node := existing
		s.writeDevContent(&node, content, actor.UserID)
		if err := s.repo.UpdateDevDocNode(&node); err != nil {
			return nil, false, err
		}
		return &node, false, nil
	}
	node, err := s.createImportedDevDocument(projectID, currentParent, docName, content, actor.UserID)
	if err != nil {
		return nil, false, err
	}
	return node, true, nil
}

// GetDevDocByPath resolves doc_dir/doc_name and returns the document node (open read API).
func (s *ProjectService) GetDevDocByPath(actor AccessContext, projectID uint, docDir, docName string) (*projectmodel.DevDocNode, error) {
	if _, err := s.acl.Require(projectID, actor, "project_dev_docs:view", capDevDocView); err != nil {
		return nil, err
	}
	dirs, err := parseRelDirPath(docDir, "doc_dir")
	if err != nil {
		return nil, err
	}
	docName, err = normalizeMDFileName(docName, "doc_name")
	if err != nil {
		return nil, err
	}
	nodes, err := s.repo.ListDevDocNodes(projectID)
	if err != nil {
		return nil, err
	}
	index := newDevDocImportIndex(nodes, nil)
	key := "root"
	for _, directory := range dirs {
		key = key + "/" + directory
		existing, ok := index[key]
		if !ok || existing.Kind != projectmodel.DocNodeDirectory {
			return nil, NewNotFound("文档路径不存在")
		}
	}
	documentKey := key + "/" + docName
	existing, ok := index[documentKey]
	if !ok || existing.Kind != projectmodel.DocNodeDocument {
		return nil, NewNotFound("文档路径不存在")
	}
	return &existing, nil
}

// DevDocExportItem is one document in a flat export listing (relative path + content).
type DevDocExportItem struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// ExportDevDocs returns all document nodes under doc_dir (empty = project root) as a flat list.
// Missing doc_dir yields an empty list; invalid doc_dir yields a parse error.
func (s *ProjectService) ExportDevDocs(actor AccessContext, projectID uint, docDir string) ([]DevDocExportItem, error) {
	if _, err := s.acl.Require(projectID, actor, "project_dev_docs:view", capDevDocView); err != nil {
		return nil, err
	}
	dirs, err := parseRelDirPath(docDir, "doc_dir")
	if err != nil {
		return nil, err
	}
	nodes, err := s.repo.ListDevDocNodes(projectID)
	if err != nil {
		return nil, err
	}
	index := newDevDocImportIndex(nodes, nil)
	exportRoot := "root"
	for _, directory := range dirs {
		exportRoot = exportRoot + "/" + directory
	}
	if len(dirs) > 0 {
		existing, ok := index[exportRoot]
		if !ok || existing.Kind != projectmodel.DocNodeDirectory {
			return []DevDocExportItem{}, nil
		}
	}
	prefix := exportRoot + "/"
	items := make([]DevDocExportItem, 0)
	for key, node := range index {
		if node.Kind != projectmodel.DocNodeDocument {
			continue
		}
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rel := strings.TrimPrefix(key, prefix)
		if rel == "" {
			continue
		}
		items = append(items, DevDocExportItem{Path: rel, Content: node.Content})
	}
	slices.SortFunc(items, func(a, b DevDocExportItem) int {
		return strings.Compare(a.Path, b.Path)
	})
	return items, nil
}

func (s *ProjectService) validateDevDocParent(projectID uint, parentID *uint) error {
	if parentID == nil {
		return nil
	}
	parent, err := s.repo.FindDevDocNode(*parentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewNotFound("父节点不存在")
	}
	if err != nil {
		return err
	}
	if parent.ProjectID != projectID {
		return errors.New("父节点不属于当前项目")
	}
	if parent.Kind != projectmodel.DocNodeDirectory {
		return errors.New("父节点必须为目录")
	}
	return nil
}

func (s *ProjectService) writeDevContent(node *projectmodel.DevDocNode, content string, userID uint) {
	node.Content = content
	node.UpdatedBy = userID
}

func (s *ProjectService) createImportedDevDocument(projectID uint, parentID *uint, name, content string, userID uint) (*projectmodel.DevDocNode, error) {
	if name == "" {
		return nil, errors.New("无效 Markdown 文件名")
	}
	node := &projectmodel.DevDocNode{
		ProjectID: projectID, ParentID: parentID, Kind: projectmodel.DocNodeDocument, Name: name,
		Content: content, CreatedBy: userID, UpdatedBy: userID,
	}
	if err := s.repo.CreateDevDocNode(node); err != nil {
		return nil, err
	}
	return node, nil
}

func buildDevDocTree(nodes []projectmodel.DevDocNode) []projectmodel.DevDocNode {
	byID := make(map[uint]*projectmodel.DevDocNode, len(nodes))
	for i := range nodes {
		nodes[i].Children = nil
		byID[nodes[i].ID] = &nodes[i]
	}
	roots := make([]projectmodel.DevDocNode, 0)
	for _, node := range nodes {
		current := byID[node.ID]
		if current.ParentID == nil {
			roots = append(roots, *current)
			continue
		}
		parent, ok := byID[*current.ParentID]
		if !ok {
			roots = append(roots, *current)
			continue
		}
		parent.Children = append(parent.Children, *current)
	}
	var materialize func(projectmodel.DevDocNode) projectmodel.DevDocNode
	materialize = func(node projectmodel.DevDocNode) projectmodel.DevDocNode {
		if current, ok := byID[node.ID]; ok {
			node = *current
		}
		for i := range node.Children {
			node.Children[i] = materialize(node.Children[i])
		}
		return node
	}
	for i := range roots {
		roots[i] = materialize(roots[i])
	}
	return roots
}

func devDocSubtreeIDs(nodes []projectmodel.DevDocNode, rootID uint) []uint {
	children := make(map[uint][]uint)
	for _, node := range nodes {
		if node.ParentID != nil {
			children[*node.ParentID] = append(children[*node.ParentID], node.ID)
		}
	}
	ids := []uint{rootID}
	for index := 0; index < len(ids); index++ {
		ids = append(ids, children[ids[index]]...)
	}
	return ids
}

func newDevDocImportIndex(nodes []projectmodel.DevDocNode, rootParent *uint) map[string]projectmodel.DevDocNode {
	index := map[string]projectmodel.DevDocNode{}
	byID := make(map[uint]projectmodel.DevDocNode, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	var nodePath func(uint) string
	nodePath = func(id uint) string {
		node, ok := byID[id]
		if !ok {
			return "root"
		}
		parentPath := "root"
		if node.ParentID != nil {
			parentPath = nodePath(*node.ParentID)
		}
		return parentPath + "/" + node.Name
	}
	for _, node := range nodes {
		index[nodePath(node.ID)] = node
	}
	_ = rootParent
	return index
}

func importDevParentPath(nodes []projectmodel.DevDocNode, parentID *uint) string {
	if parentID == nil {
		return "root"
	}
	byID := make(map[uint]projectmodel.DevDocNode, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	var nodePath func(uint) string
	nodePath = func(id uint) string {
		node, ok := byID[id]
		if !ok {
			return "root"
		}
		parentPath := "root"
		if node.ParentID != nil {
			parentPath = nodePath(*node.ParentID)
		}
		return parentPath + "/" + node.Name
	}
	return nodePath(*parentID)
}
