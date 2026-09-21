package bedctl

import (
	"os"
	"path/filepath"
	"testing"
)

// procFixture builds a /proc-like tree with one listening socket on port
// 8080 (inode 12345) owned by pid 1234 and a stray socket owned by nobody.
func procFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	tcp := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 00000000:1F90 00000000:0000 0A 00000000:0000 00:00000000 00000000     0        0 12345 1 ffff\n" +
		"   1: 0100007A:1F91 00000000:0000 0A 00000000:0000 00:00000000 00000000     0        0 99999 1 ffff\n"
	if err := os.WriteFile(filepath.Join(root, "net", "tcp"), []byte(tcp), 0o644); err != nil {
		t.Fatal(err)
	}
	pidDir := filepath.Join(root, "1234", "fd")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[12345]", filepath.Join(pidDir, "3")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestParseListenInodes(t *testing.T) {
	root := procFixture(t)
	inodes, err := parseListenInodes(root, 8080)
	if err != nil {
		t.Fatal(err)
	}
	if len(inodes) != 1 || inodes[0].Inode != "12345" {
		t.Fatalf("inodes = %+v, want single inode 12345", inodes)
	}
	if inodes, _ := parseListenInodes(root, 9999); len(inodes) != 0 {
		t.Fatalf("port 9999 should have no listeners, got %+v", inodes)
	}
}

func TestPIDsListeningOnPort(t *testing.T) {
	root := procFixture(t)
	pids := pidsListeningOnPort(root, 8080)
	if len(pids) != 1 || pids[0] != 1234 {
		t.Fatalf("pids = %v, want [1234]", pids)
	}
	if pids := pidsListeningOnPort(root, 9999); len(pids) != 0 {
		t.Fatalf("pids = %v, want none", pids)
	}
}

func TestPIDsRunningBinary(t *testing.T) {
	root := t.TempDir()
	pidDir := filepath.Join(root, "4321")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// exe link pointing at the binary (symlink target string compare).
	if err := os.Symlink("/opt/bedrock/bedrock", filepath.Join(pidDir, "exe")); err != nil {
		t.Fatal(err)
	}
	pids := pidsRunningBinary(root, "/opt/bedrock/bedrock")
	if len(pids) != 1 || pids[0] != 4321 {
		t.Fatalf("pids = %v, want [4321]", pids)
	}
	// A different binary must not match.
	if pids := pidsRunningBinary(root, "/opt/bedrock/bedrock-agent"); len(pids) != 0 {
		t.Fatalf("pids = %v, want none", pids)
	}
}

func TestPIDCmdLineIdentity(t *testing.T) {
	root := t.TempDir()
	pidDir := filepath.Join(root, "777")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "cmdline"), []byte("/opt/bedrock/bedrock\x00--config\x00/opt/bedrock/config.yaml\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := pidCmdLine(root, 777)
	if got != "/opt/bedrock/bedrock --config /opt/bedrock/config.yaml" {
		t.Fatalf("cmdline = %q", got)
	}
}
