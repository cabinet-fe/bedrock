package deployer

import (
	"strings"
	"testing"
)

func TestBuildRemoteSSHCLI_passwordUsesSSHPass(t *testing.T) {
	t.Parallel()
	name, args, cleanup := buildRemoteSSHCLI(ServerInfo{
		Host:     "example.test",
		Port:     22,
		Username: "deploy",
		AuthType: "password",
		Password: "s3cret",
	}, "sh -lc 'echo hi'")
	defer cleanup()

	if name != "sshpass" {
		t.Fatalf("name=%q want sshpass", name)
	}
	if len(args) < 4 || args[0] != "-p" || args[1] != "s3cret" || args[2] != "ssh" {
		t.Fatalf("args prefix=%v", args)
	}
	assertSSHTargetAndCommand(t, args, "deploy@example.test", "sh -lc 'echo hi'")
	if containsArg(args, "IdentityFile=") {
		t.Fatalf("password auth should not set IdentityFile: %v", args)
	}
}

func TestBuildRemoteSSHCLI_sshKeyUsesBareSSH(t *testing.T) {
	t.Parallel()
	name, args, cleanup := buildRemoteSSHCLI(ServerInfo{
		Host:     "example.test",
		Port:     22,
		Username: "deploy",
		AuthType: "ssh_key",
	}, "sh -lc 'restart'")
	defer cleanup()

	if name != "ssh" {
		t.Fatalf("name=%q want ssh", name)
	}
	if len(args) > 0 && args[0] == "-p" {
		t.Fatalf("ssh_key must not use sshpass -p: %v", args)
	}
	assertSSHTargetAndCommand(t, args, "deploy@example.test", "sh -lc 'restart'")
}

func TestBuildRemoteSSHCLI_nonDefaultPort(t *testing.T) {
	t.Parallel()
	_, args, cleanup := buildRemoteSSHCLI(ServerInfo{
		Host:     "example.test",
		Port:     2222,
		Username: "deploy",
		AuthType: "ssh_key",
	}, "true")
	defer cleanup()

	found := false
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-o" && args[i+1] == "Port=2222" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Port=2222 in args: %v", args)
	}
}

func TestBuildRemoteSSHCLI_embeddedKeyWritesIdentityFile(t *testing.T) {
	t.Parallel()
	pem := "-----BEGIN OPENSSH PRIVATE KEY-----\ntest\n-----END OPENSSH PRIVATE KEY-----"
	_, args, cleanup := buildRemoteSSHCLI(ServerInfo{
		Host:       "example.test",
		Username:   "deploy",
		AuthType:   "ssh_key",
		PrivateKey: pem,
	}, "true")
	defer cleanup()

	if !containsArg(args, "IdentityFile=") {
		t.Fatalf("expected IdentityFile option: %v", args)
	}
}

func assertSSHTargetAndCommand(t *testing.T, args []string, wantTarget, wantCommand string) {
	t.Helper()
	dashDash := -1
	for i, a := range args {
		if a == "--" {
			dashDash = i
			break
		}
	}
	if dashDash < 1 || dashDash+1 >= len(args) {
		t.Fatalf("missing target/--/command in %v", args)
	}
	if args[dashDash-1] != wantTarget {
		t.Fatalf("target=%q want %q (args=%v)", args[dashDash-1], wantTarget, args)
	}
	if args[dashDash+1] != wantCommand {
		t.Fatalf("command=%q want %q", args[dashDash+1], wantCommand)
	}
}

func containsArg(args []string, substr string) bool {
	for _, a := range args {
		if strings.Contains(a, substr) {
			return true
		}
	}
	return false
}
