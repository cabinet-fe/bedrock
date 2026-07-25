package pkg

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"testing"
)

func encryptAES256CBCHexForTest(plaintext string) (string, error) {
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", err
	}
	padded := pkcs7Pad([]byte(plaintext), aes.BlockSize)
	iv := make([]byte, aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
	return hex.EncodeToString(append(iv, ciphertext...)), nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padLen := blockSize - len(data)%blockSize
	return append(data, bytes.Repeat([]byte{byte(padLen)}, padLen)...)
}

func TestDecryptLoginPasswordCipher_roundTrip(t *testing.T) {
	const keyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := InitEncryption(keyHex); err != nil {
		t.Fatal(err)
	}
	plain := "secret-password-你好"
	enc, err := encryptAES256CBCHexForTest(plain)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecryptLoginPasswordCipher(enc)
	if err != nil {
		t.Fatal(err)
	}
	if got != plain {
		t.Fatalf("got %q want %q", got, plain)
	}
}

func TestDecryptLoginPasswordCipher_invalidHex(t *testing.T) {
	const keyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := InitEncryption(keyHex); err != nil {
		t.Fatal(err)
	}
	_, err := DecryptLoginPasswordCipher("gg")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecryptLoginPasswordCipher_tooShort(t *testing.T) {
	const keyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := InitEncryption(keyHex); err != nil {
		t.Fatal(err)
	}
	// valid hex but < 32 bytes raw
	_, err := DecryptLoginPasswordCipher("00112233445566778899aabbccddeeff")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecryptLoginPasswordCipher_badPadding(t *testing.T) {
	const keyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := InitEncryption(keyHex); err != nil {
		t.Fatal(err)
	}
	// 32 bytes: IV + one block, but garbage padding
	iv := make([]byte, 16)
	for i := range iv {
		iv[i] = byte(i)
	}
	block := make([]byte, 16)
	for i := range block {
		block[i] = 0xff
	}
	bad := append(iv, block...)
	_, err := DecryptLoginPasswordCipher(hex.EncodeToString(bad))
	if err == nil {
		t.Fatal("expected error")
	}
}
