package deployer

import "testing"

func TestIsSSHKeyAuth(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"ssh_key":  true,
		"key":      true,
		"SSH_KEY":  true,
		"password": false,
		"agent":    false,
		"":         false,
	}
	for in, want := range cases {
		if got := IsSSHKeyAuth(in); got != want {
			t.Fatalf("IsSSHKeyAuth(%q)=%v want %v", in, got, want)
		}
	}
}

func TestSSHAuthMethods_password(t *testing.T) {
	t.Parallel()
	methods, err := SSHAuthMethods(ServerInfo{AuthType: "password", Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) == 0 {
		t.Fatal("expected password auth method")
	}
}

func TestSSHAuthMethods_sshKeyWithoutPEMUsesAgentOrFails(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	_, err := SSHAuthMethods(ServerInfo{AuthType: "ssh_key", PrivateKey: ""})
	if err == nil {
		t.Fatal("expected error when ssh_key has no PEM and no SSH_AUTH_SOCK")
	}
}

func TestSSHAuthMethods_legacyKeyAlias(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	_, err := SSHAuthMethods(ServerInfo{AuthType: "key", PrivateKey: ""})
	if err == nil {
		t.Fatal("legacy key without PEM/agent should fail like ssh_key")
	}
}
