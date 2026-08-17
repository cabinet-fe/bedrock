package deployer

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestIsSSHKeyAuth(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"ssh_key":  true,
		"key":      false,
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

func TestSSHAuthMethods_sshKeyWithoutPEMOrKeysUsesAgentOrFails(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("HOME", t.TempDir())
	_, err := SSHAuthMethods(ServerInfo{AuthType: "ssh_key", PrivateKey: ""})
	if err == nil {
		t.Fatal("expected error when ssh_key has no PEM, no SSH_AUTH_SOCK, and no ~/.ssh keys")
	}
}

func TestSSHAuthMethods_sshKeyLoadsDefaultUserKey(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	sshDir := filepath.Join(tempHome, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})

	keyPath := filepath.Join(sshDir, "id_ed25519")
	if err := os.WriteFile(keyPath, pemBytes, 0600); err != nil {
		t.Fatal(err)
	}

	methods, err := SSHAuthMethods(ServerInfo{AuthType: "ssh_key", PrivateKey: ""})
	if err != nil {
		t.Fatalf("expected successful auth method resolution, got: %v", err)
	}
	if len(methods) == 0 {
		t.Fatal("expected at least one auth method from ~/.ssh/id_ed25519")
	}
}

func TestSSHAuthMethods_sshKeyExplicitPrivateKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})

	methods, err := SSHAuthMethods(ServerInfo{AuthType: "ssh_key", PrivateKey: string(pemBytes)})
	if err != nil {
		t.Fatalf("expected successful auth with explicit private key: %v", err)
	}
	if len(methods) == 0 {
		t.Fatal("expected public key auth method")
	}
}
