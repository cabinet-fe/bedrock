package service_test

import (
	"bytes"
	"testing"

	"bedrock/internal/ai/model"
	"bedrock/internal/ai/service"
	rbacmodel "bedrock/internal/rbac/model"
)

func TestSkillFileCRUDAndBuiltinReadOnly(t *testing.T) {
	gdb, _, skills, _ := setupAI(t)
	z := zipBytes(t, map[string]string{
		"SKILL.md":        "# hello\n",
		"scripts/run.sh":  "echo hi\n",
		"refs/example.md": "note\n",
	})
	skill, err := skills.Create(service.SkillUploadInput{
		Name: "edit-me", Visibility: model.SkillPrivate, Filename: "s.zip",
		Size: int64(len(z)), Source: bytes.NewReader(z), UserID: 1, IsSuperAdmin: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !skill.Editable || skill.Source != model.SkillSourceUploaded {
		t.Fatalf("uploaded skill editable=%v source=%q", skill.Editable, skill.Source)
	}

	tree, err := skills.ListFiles(skill.ID, 1, true, rbacmodel.DataScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) < 2 {
		t.Fatalf("tree=%+v", tree)
	}

	content, err := skills.ReadFile(skill.ID, 1, true, rbacmodel.DataScopeAll, "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if content.Content != "# hello\n" {
		t.Fatalf("content=%q", content.Content)
	}

	written, err := skills.WriteFile(skill.ID, 1, true, rbacmodel.DataScopeAll, service.SkillWriteFileInput{
		Path: "SKILL.md", Content: "# edited\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if written.Content != "# edited\n" {
		t.Fatalf("written=%q", written.Content)
	}

	got, err := skills.Get(skill.ID, 1, true, rbacmodel.DataScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if got.PackageDigest == skill.PackageDigest {
		t.Fatal("expected package digest to change after write")
	}

	node, err := skills.CreateEntry(skill.ID, 1, true, rbacmodel.DataScopeAll, service.SkillCreateEntryInput{
		Path: "notes/new.md", Kind: "file", Content: "x\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.Path != "notes/new.md" {
		t.Fatalf("node=%+v", node)
	}

	if _, err := skills.CreateEntry(skill.ID, 1, true, rbacmodel.DataScopeAll, service.SkillCreateEntryInput{
		Path: "notes/subdir", Kind: "dir",
	}); err != nil {
		t.Fatal(err)
	}

	renamed, err := skills.RenameEntry(skill.ID, 1, true, rbacmodel.DataScopeAll, service.SkillRenameInput{
		FromPath: "notes/new.md", ToPath: "notes/renamed.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Path != "notes/renamed.md" {
		t.Fatalf("renamed=%+v", renamed)
	}

	if err := skills.DeleteEntry(skill.ID, 1, true, rbacmodel.DataScopeAll, "notes/renamed.md"); err != nil {
		t.Fatal(err)
	}
	if err := skills.DeleteEntry(skill.ID, 1, true, rbacmodel.DataScopeAll, "SKILL.md"); err == nil {
		t.Fatal("expected reject deleting SKILL.md")
	}
	if _, err := skills.ReadFile(skill.ID, 1, true, rbacmodel.DataScopeAll, "../etc/passwd"); err == nil {
		t.Fatal("expected path traversal reject")
	}

	builtinZIP := zipBytes(t, map[string]string{"SKILL.md": "# builtin\n"})
	builtin, err := skills.Create(service.SkillUploadInput{
		Name: "builtin-skill", Visibility: model.SkillPublic, Filename: "b.zip",
		Size: int64(len(builtinZIP)), Source: bytes.NewReader(builtinZIP), UserID: 1, IsSuperAdmin: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&model.SkillPackage{}).Where("id = ?", builtin.ID).
		Update("source", model.SkillSourceBuiltin).Error; err != nil {
		t.Fatal(err)
	}

	gotBuiltin, err := skills.Get(builtin.ID, 1, true, rbacmodel.DataScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if gotBuiltin.Editable || gotBuiltin.Source != model.SkillSourceBuiltin {
		t.Fatalf("builtin editable=%v source=%q", gotBuiltin.Editable, gotBuiltin.Source)
	}
	if _, err := skills.ListFiles(builtin.ID, 1, true, rbacmodel.DataScopeAll); err != nil {
		t.Fatal(err)
	}
	if _, err := skills.WriteFile(builtin.ID, 1, true, rbacmodel.DataScopeAll, service.SkillWriteFileInput{
		Path: "SKILL.md", Content: "# no\n",
	}); err == nil {
		t.Fatal("expected builtin write reject")
	}
	if err := skills.Delete(builtin.ID, 1, true); err == nil {
		t.Fatal("expected builtin delete reject")
	}
}
