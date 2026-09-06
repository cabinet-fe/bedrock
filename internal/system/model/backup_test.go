package model_test

import (
	"encoding/json"
	"testing"
	"time"

	"bedrock/internal/system/model"
)

func TestIsValidBackupModule(t *testing.T) {
	for _, m := range model.AllBackupModules {
		if !model.IsValidBackupModule(m) {
			t.Errorf("expected module %s to be valid", m)
		}
	}
	if model.IsValidBackupModule("invalid_module") {
		t.Error("expected invalid_module to be invalid")
	}
}

func TestSystemBackup_TableName(t *testing.T) {
	b := model.SystemBackup{}
	if b.TableName() != "system_backups" {
		t.Errorf("expected table name system_backups, got %s", b.TableName())
	}
}

func TestSystemBackup_ModulesEncodingDecoding(t *testing.T) {
	b := &model.SystemBackup{
		Modules: []string{model.BackupModuleDatabase, model.BackupModuleConfig},
	}
	if err := b.EncodeModules(); err != nil {
		t.Fatalf("EncodeModules failed: %v", err)
	}
	if b.ModulesJSON != `["database","config"]` {
		t.Errorf("unexpected ModulesJSON: %s", b.ModulesJSON)
	}

	b2 := &model.SystemBackup{
		ModulesJSON: `["storage","artifacts","logs"]`,
	}
	b2.DecodeModules()
	if len(b2.Modules) != 3 || b2.Modules[0] != "storage" || b2.Modules[1] != "artifacts" || b2.Modules[2] != "logs" {
		t.Errorf("unexpected Modules: %#v", b2.Modules)
	}

	b3 := &model.SystemBackup{
		ModulesJSON: "invalid json",
	}
	b3.DecodeModules()
	if len(b3.Modules) != 0 {
		t.Errorf("expected empty slice for invalid json, got %#v", b3.Modules)
	}

	b4 := &model.SystemBackup{
		Modules: []string{model.BackupModuleDatabase},
	}
	if err := b4.BeforeSave(nil); err != nil {
		t.Fatalf("BeforeSave failed: %v", err)
	}
	if b4.ModulesJSON != `["database"]` {
		t.Errorf("unexpected ModulesJSON after BeforeSave: %s", b4.ModulesJSON)
	}

	b5 := &model.SystemBackup{
		ModulesJSON: `["database"]`,
	}
	if err := b5.AfterFind(nil); err != nil {
		t.Fatalf("AfterFind failed: %v", err)
	}
	if len(b5.Modules) != 1 || b5.Modules[0] != "database" {
		t.Errorf("unexpected Modules after AfterFind: %#v", b5.Modules)
	}
}

func TestSystemBackupRestoreRequest_ShouldAutoSnapshot(t *testing.T) {
	reqNil := model.SystemBackupRestoreRequest{}
	if !reqNil.ShouldAutoSnapshot() {
		t.Error("expected default auto snapshot to be true when nil")
	}

	tVal := true
	reqTrue := model.SystemBackupRestoreRequest{AutoSnapshot: &tVal}
	if !reqTrue.ShouldAutoSnapshot() {
		t.Error("expected auto snapshot to be true")
	}

	fVal := false
	reqFalse := model.SystemBackupRestoreRequest{AutoSnapshot: &fVal}
	if reqFalse.ShouldAutoSnapshot() {
		t.Error("expected auto snapshot to be false")
	}
}

func TestBackupManifest_JSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	m := model.BackupManifest{
		Version:    model.BackupManifestVersion,
		AppVersion: "1.0.0",
		DBDriver:   "sqlite",
		Modules:    []string{model.BackupModuleDatabase, model.BackupModuleConfig},
		CreatedAt:  now,
		Note:       "manual test backup",
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal BackupManifest failed: %v", err)
	}

	var parsed model.BackupManifest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal BackupManifest failed: %v", err)
	}

	if parsed.Version != m.Version || parsed.DBDriver != m.DBDriver || len(parsed.Modules) != 2 {
		t.Errorf("parsed manifest mismatch: %#v", parsed)
	}
}
