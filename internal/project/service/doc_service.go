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

const (
	maxZIPEntries = 1000
	maxZIPRatio   = 100
)

type DocNodeInput struct {
	ParentID     *uint   `json:"parent_id"`
	Kind         string  `json:"kind"`
	Name         string  `json:"name"`
	SortOrder    int     `json:"sort_order"`
	RepositoryID *uint   `json:"repository_id"`
	Content      *string `json:"content"`
}

type DocMoveInput struct {
	ParentID  *uint `json:"parent_id"`
	SortOrder int   `json:"sort_order"`
}

func (s *ProjectService) ListDocTree(actor AccessContext, projectID uint) ([]projectmodel.ApiDocNode, error) {
	if _, err := s.acl.Require(projectID, actor, "project_docs:view", capDocView); err != nil {
		return nil, err
	}
	nodes, err := s.repo.ListDocTreeNodes(projectID)
	if err != nil {
		return nil, err
	}
	return buildDocTree(nodes), nil
}

func (s *ProjectService) GetDocNode(actor AccessContext, id uint) (*projectmodel.ApiDocNode, error) {
	node, err := s.repo.FindDocNode(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("文档节点不存在")
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.acl.Require(node.ProjectID, actor, "project_docs:view", capDocView); err != nil {
		return nil, err
	}
	return node, nil
}

// CheckDocProject validates nested document routes without imposing a separate
// global :view grant on callers that only hold a write permission.
func (s *ProjectService) CheckDocProject(actor AccessContext, projectID, nodeID uint, globalPermission string, write bool) error {
	node, err := s.repo.FindDocNode(nodeID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewNotFound("文档节点不存在")
	}
	if err != nil {
		return err
	}
	if node.ProjectID != projectID {
		return NewNotFound("文档节点不存在")
	}
	capability := capDocView
	if write {
		capability = capDocEdit
	}
	_, err = s.acl.Require(projectID, actor, globalPermission, capability)
	return err
}

func (s *ProjectService) CreateDocNode(actor AccessContext, projectID uint, input DocNodeInput) (*projectmodel.ApiDocNode, error) {
	if _, err := s.acl.Require(projectID, actor, "project_docs:create", capDocEdit); err != nil {
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
	if err := s.validateDocParent(projectID, input.ParentID); err != nil {
		return nil, err
	}
	node := &projectmodel.ApiDocNode{
		ProjectID: projectID, ParentID: input.ParentID, Kind: kind, Name: name, SortOrder: input.SortOrder,
		RepositoryID: input.RepositoryID, CreatedBy: actor.UserID, UpdatedBy: actor.UserID,
	}
	if kind == projectmodel.DocNodeDocument && input.Content != nil {
		node.Content = *input.Content
	}
	if err := s.repo.CreateDocNode(node); err != nil {
		return nil, err
	}
	return node, nil
}

func (s *ProjectService) UpdateDocNode(actor AccessContext, id uint, input DocNodeInput) (*projectmodel.ApiDocNode, error) {
	node, err := s.repo.FindDocNode(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("文档节点不存在")
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.acl.Require(node.ProjectID, actor, "project_docs:update", capDocEdit); err != nil {
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
		s.writeContent(node, *input.Content, actor.UserID)
	}
	node.UpdatedBy = actor.UserID
	if err := s.repo.UpdateDocNode(node); err != nil {
		return nil, err
	}
	return node, nil
}

func (s *ProjectService) MoveDocNode(actor AccessContext, id uint, input DocMoveInput) (*projectmodel.ApiDocNode, error) {
	node, err := s.repo.FindDocNode(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NewNotFound("文档节点不存在")
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.acl.Require(node.ProjectID, actor, "project_docs:update", capDocEdit); err != nil {
		return nil, err
	}
	if err := s.requireActiveProject(node.ProjectID); err != nil {
		return nil, err
	}
	if err := s.validateDocParent(node.ProjectID, input.ParentID); err != nil {
		return nil, err
	}
	if input.ParentID != nil && *input.ParentID == node.ID {
		return nil, errors.New("节点不能移动到自身")
	}
	nodes, err := s.repo.ListDocNodes(node.ProjectID)
	if err != nil {
		return nil, err
	}
	descendants := docSubtreeIDs(nodes, node.ID)
	if input.ParentID != nil {
		if slices.Contains(descendants, *input.ParentID) {
			return nil, errors.New("节点不能移动到自己的子节点")
		}
	}
	node.ParentID = input.ParentID
	node.SortOrder = input.SortOrder
	node.UpdatedBy = actor.UserID
	if err := s.repo.UpdateDocNode(node); err != nil {
		return nil, err
	}
	return node, nil
}

func (s *ProjectService) DeleteDocNode(actor AccessContext, id uint) error {
	node, err := s.repo.FindDocNode(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewNotFound("文档节点不存在")
	}
	if err != nil {
		return err
	}
	if _, err := s.acl.Require(node.ProjectID, actor, "project_docs:delete", capDocAdmin); err != nil {
		return err
	}
	nodes, err := s.repo.ListDocNodes(node.ProjectID)
	if err != nil {
		return err
	}
	return s.repo.DeleteDocNodes(docSubtreeIDs(nodes, id))
}

func (s *ProjectService) UploadMarkdown(actor AccessContext, projectID uint, parentID *uint, filename, contentType string, source io.Reader, size int64) (*projectmodel.ApiDocNode, error) {
	if _, err := s.acl.Require(projectID, actor, "project_docs:create", capDocEdit); err != nil {
		return nil, err
	}
	if err := s.requireActiveProject(projectID); err != nil {
		return nil, err
	}
	if !strings.EqualFold(path.Ext(filename), ".md") {
		return nil, errors.New("仅支持上传 .md 文件")
	}
	if err := s.validateDocParent(projectID, parentID); err != nil {
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
	return s.createImportedDocument(projectID, parentID, safeDocName(path.Base(filename)), string(content), actor.UserID)
}

func (s *ProjectService) ImportZIP(actor AccessContext, projectID uint, parentID *uint, filename, contentType string, source io.Reader, size int64) ([]projectmodel.ApiDocNode, error) {
	if _, err := s.acl.Require(projectID, actor, "project_docs:create", capDocEdit); err != nil {
		return nil, err
	}
	if err := s.requireActiveProject(projectID); err != nil {
		return nil, err
	}
	if !strings.EqualFold(path.Ext(filename), ".zip") {
		return nil, errors.New("仅支持上传 .zip 文件")
	}
	if err := s.validateDocParent(projectID, parentID); err != nil {
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

	nodes, err := s.repo.ListDocNodes(projectID)
	if err != nil {
		return nil, err
	}
	index := newDocImportIndex(nodes, parentID)
	imported := make([]projectmodel.ApiDocNode, 0)
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
		currentKey := importParentPath(nodes, parentID)
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
			directoryNode := &projectmodel.ApiDocNode{
				ProjectID: projectID, ParentID: currentParent, Kind: projectmodel.DocNodeDirectory, Name: directory,
				CreatedBy: actor.UserID, UpdatedBy: actor.UserID,
			}
			if err := s.repo.CreateDocNode(directoryNode); err != nil {
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
			s.writeContent(&node, string(content), actor.UserID)
			if err := s.repo.UpdateDocNode(&node); err != nil {
				return nil, err
			}
			index[documentKey] = node
			imported = append(imported, node)
			continue
		}
		node, err := s.createImportedDocument(projectID, currentParent, documentName, string(content), actor.UserID)
		if err != nil {
			return nil, err
		}
		index[documentKey] = *node
		imported = append(imported, *node)
	}
	return imported, nil
}

// UpsertDocByPath creates missing directories under api_dir and upserts a document by name.
// created is true when a new document node was inserted.
func (s *ProjectService) UpsertDocByPath(actor AccessContext, projectID uint, apiDir, apiDocName, content string) (*projectmodel.ApiDocNode, bool, error) {
	if _, err := s.acl.Require(projectID, actor, "project_docs:create", capDocEdit); err != nil {
		return nil, false, err
	}
	if err := s.requireActiveProject(projectID); err != nil {
		return nil, false, err
	}
	dirs, err := parseDocDirPath(apiDir)
	if err != nil {
		return nil, false, err
	}
	docName, err := normalizeDocFileName(apiDocName)
	if err != nil {
		return nil, false, err
	}

	nodes, err := s.repo.ListDocNodes(projectID)
	if err != nil {
		return nil, false, err
	}
	index := newDocImportIndex(nodes, nil)
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
		directoryNode := &projectmodel.ApiDocNode{
			ProjectID: projectID, ParentID: currentParent, Kind: projectmodel.DocNodeDirectory, Name: directory,
			CreatedBy: actor.UserID, UpdatedBy: actor.UserID,
		}
		if err := s.repo.CreateDocNode(directoryNode); err != nil {
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
		s.writeContent(&node, content, actor.UserID)
		if err := s.repo.UpdateDocNode(&node); err != nil {
			return nil, false, err
		}
		return &node, false, nil
	}
	node, err := s.createImportedDocument(projectID, currentParent, docName, content, actor.UserID)
	if err != nil {
		return nil, false, err
	}
	return node, true, nil
}

// GetDocByPath resolves api_dir/api_doc_name and returns the document node (open read API).
func (s *ProjectService) GetDocByPath(actor AccessContext, projectID uint, apiDir, apiDocName string) (*projectmodel.ApiDocNode, error) {
	if _, err := s.acl.Require(projectID, actor, "project_docs:view", capDocView); err != nil {
		return nil, err
	}
	dirs, err := parseDocDirPath(apiDir)
	if err != nil {
		return nil, err
	}
	docName, err := normalizeDocFileName(apiDocName)
	if err != nil {
		return nil, err
	}
	nodes, err := s.repo.ListDocNodes(projectID)
	if err != nil {
		return nil, err
	}
	index := newDocImportIndex(nodes, nil)
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

// DocExportItem is one document in a flat export listing (relative path + content).
type DocExportItem struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// ExportDocs returns all document nodes under api_dir (empty = project root) as a flat list.
// Missing api_dir yields an empty list; invalid api_dir yields a parse error.
func (s *ProjectService) ExportDocs(actor AccessContext, projectID uint, apiDir string) ([]DocExportItem, error) {
	if _, err := s.acl.Require(projectID, actor, "project_docs:view", capDocView); err != nil {
		return nil, err
	}
	dirs, err := parseDocDirPath(apiDir)
	if err != nil {
		return nil, err
	}
	nodes, err := s.repo.ListDocNodes(projectID)
	if err != nil {
		return nil, err
	}
	index := newDocImportIndex(nodes, nil)
	exportRoot := "root"
	for _, directory := range dirs {
		exportRoot = exportRoot + "/" + directory
	}
	if len(dirs) > 0 {
		existing, ok := index[exportRoot]
		if !ok || existing.Kind != projectmodel.DocNodeDirectory {
			return []DocExportItem{}, nil
		}
	}
	prefix := exportRoot + "/"
	items := make([]DocExportItem, 0)
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
		items = append(items, DocExportItem{Path: rel, Content: node.Content})
	}
	slices.SortFunc(items, func(a, b DocExportItem) int {
		return strings.Compare(a.Path, b.Path)
	})
	return items, nil
}

type GenerateDocsInput struct {
	AgentID uint  `json:"agent_id"`
	NodeID  *uint `json:"node_id"`
}

type GenerateDocsResult struct {
	AgentRunID uint `json:"agent_run_id"`
	NodeID     uint `json:"node_id"`
}

func (s *ProjectService) GenerateDocs(actor AccessContext, projectID uint, input GenerateDocsInput) (*GenerateDocsResult, error) {
	if _, err := s.acl.Require(projectID, actor, "project_docs:execute", capDocEdit); err != nil {
		return nil, err
	}
	if s.docsAI == nil {
		return nil, ErrAIDomainUnavailable
	}
	if input.AgentID == 0 {
		return nil, errors.New("agent_id 不能为空")
	}
	var nodeID uint
	if input.NodeID != nil {
		node, err := s.repo.FindDocNode(*input.NodeID)
		if err != nil {
			return nil, NewNotFound("文档节点不存在")
		}
		if node.ProjectID != projectID || node.Kind != projectmodel.DocNodeDocument {
			return nil, errors.New("node_id 必须指向本项目的文档节点")
		}
		nodeID = node.ID
	} else {
		// Create an empty document to receive generated content.
		node, err := s.createImportedDocument(projectID, nil, "AI Generated", "", actor.UserID)
		if err != nil {
			return nil, err
		}
		nodeID = node.ID
	}
	runID, err := s.docsAI.StartDocsGenerate(actor.UserID, projectID, nodeID, input.AgentID)
	if err != nil {
		return nil, err
	}
	return &GenerateDocsResult{AgentRunID: runID, NodeID: nodeID}, nil
}

// WriteDraftFromAgentRun writes document content after a successful AgentRun.
func (s *ProjectService) WriteDraftFromAgentRun(projectID, nodeID, runID uint, content string, userID uint) error {
	node, err := s.repo.FindDocNode(nodeID)
	if err != nil {
		return err
	}
	if node.ProjectID != projectID {
		return errors.New("文档节点不属于当前项目")
	}
	s.writeContent(node, content, userID)
	rid := runID
	node.DraftSourceRunID = &rid
	return s.repo.UpdateDocNode(node)
}

func (s *ProjectService) validateDocParent(projectID uint, parentID *uint) error {
	if parentID == nil {
		return nil
	}
	parent, err := s.repo.FindDocNode(*parentID)
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

func (s *ProjectService) writeContent(node *projectmodel.ApiDocNode, content string, userID uint) {
	node.Content = content
	node.UpdatedBy = userID
}

func (s *ProjectService) createImportedDocument(projectID uint, parentID *uint, name, content string, userID uint) (*projectmodel.ApiDocNode, error) {
	if name == "" {
		return nil, errors.New("无效 Markdown 文件名")
	}
	node := &projectmodel.ApiDocNode{
		ProjectID: projectID, ParentID: parentID, Kind: projectmodel.DocNodeDocument, Name: name,
		Content: content, CreatedBy: userID, UpdatedBy: userID,
	}
	if err := s.repo.CreateDocNode(node); err != nil {
		return nil, err
	}
	return node, nil
}

func buildDocTree(nodes []projectmodel.ApiDocNode) []projectmodel.ApiDocNode {
	byID := make(map[uint]*projectmodel.ApiDocNode, len(nodes))
	for i := range nodes {
		nodes[i].Children = nil
		byID[nodes[i].ID] = &nodes[i]
	}
	roots := make([]projectmodel.ApiDocNode, 0)
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
	var materialize func(projectmodel.ApiDocNode) projectmodel.ApiDocNode
	materialize = func(node projectmodel.ApiDocNode) projectmodel.ApiDocNode {
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

func docSubtreeIDs(nodes []projectmodel.ApiDocNode, rootID uint) []uint {
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

func safeDocName(value string) string {
	name := strings.TrimSpace(path.Base(strings.ReplaceAll(value, "\\", "/")))
	if name == "" || name == "." || name == ".." || strings.Contains(name, "\x00") {
		return ""
	}
	return name
}

// parseDocDirPath splits a relative document directory path; empty means project root.
func parseDocDirPath(apiDir string) ([]string, error) {
	return parseRelDirPath(apiDir, "api_dir")
}

func normalizeDocFileName(apiDocName string) (string, error) {
	return normalizeMDFileName(apiDocName, "api_doc_name")
}

// parseRelDirPath splits a relative directory path; empty means project root.
func parseRelDirPath(dir, field string) ([]string, error) {
	raw := strings.TrimSpace(strings.ReplaceAll(dir, "\\", "/"))
	if raw == "" || raw == "." {
		return nil, nil
	}
	if strings.HasPrefix(raw, "/") {
		return nil, fmt.Errorf("%s 不能为绝对路径", field)
	}
	parts := strings.Split(raw, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			return nil, fmt.Errorf("%s 包含非法空段", field)
		}
		if part == ".." || strings.Contains(part, "\x00") {
			return nil, fmt.Errorf("%s 包含非法路径", field)
		}
		name := safeDocName(part)
		if name == "" || name != part {
			return nil, fmt.Errorf("%s 包含非法路径", field)
		}
		out = append(out, name)
	}
	return out, nil
}

func normalizeMDFileName(fileName, field string) (string, error) {
	name := strings.TrimSpace(fileName)
	if name == "" {
		return "", fmt.Errorf("%s 不能为空", field)
	}
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.Contains(name, "/") {
		return "", fmt.Errorf("%s 不能包含路径分隔符", field)
	}
	if !strings.EqualFold(path.Ext(name), ".md") {
		name = name + ".md"
	}
	safe := safeDocName(name)
	if safe == "" {
		return "", fmt.Errorf("%s 无效", field)
	}
	return safe, nil
}

func validateZIPEntry(entry *zip.File) error {
	clean, err := cleanZIPPath(entry.Name)
	if err != nil {
		return err
	}
	_ = clean
	if entry.UncompressedSize64 > 0 && entry.CompressedSize64 == 0 {
		return errors.New("ZIP 条目压缩比异常")
	}
	if entry.CompressedSize64 > 0 && entry.UncompressedSize64/entry.CompressedSize64 > maxZIPRatio {
		return errors.New("ZIP 条目压缩比超过限制")
	}
	return nil
}

func cleanZIPPath(value string) (string, error) {
	if value == "" || strings.Contains(value, "\x00") || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "\\") {
		return "", errors.New("ZIP 包含非法路径")
	}
	clean := path.Clean(strings.ReplaceAll(value, "\\", "/"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("ZIP 包含路径穿越")
	}
	return clean, nil
}

func readZIPEntry(entry *zip.File, maxBytes int64) ([]byte, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("ZIP 条目超过限制")
	}
	return data, nil
}

func newDocImportIndex(nodes []projectmodel.ApiDocNode, rootParent *uint) map[string]projectmodel.ApiDocNode {
	index := map[string]projectmodel.ApiDocNode{}
	byID := make(map[uint]projectmodel.ApiDocNode, len(nodes))
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

func importParentPath(nodes []projectmodel.ApiDocNode, parentID *uint) string {
	if parentID == nil {
		return "root"
	}
	byID := make(map[uint]projectmodel.ApiDocNode, len(nodes))
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
