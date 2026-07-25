package pkg

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"

	"golang.org/x/crypto/bcrypt"
)

var encryptionKey []byte

func InitEncryption(hexKey string) error {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return err
	}
	if len(key) != 32 {
		return errors.New("encryption key must be 32 bytes (64 hex chars)")
	}
	encryptionKey = key
	return nil
}

func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// DecryptLoginPasswordCipher decrypts login payload: hex( IV(16) || AES-CBC ciphertext with PKCS#7 ).
// Uses the same 32-byte key as InitEncryption. Intended for password_cipher only (not GCM storage).
func DecryptLoginPasswordCipher(hexCipher string) (string, error) {
	if len(encryptionKey) != 32 {
		return "", errors.New("encryption not initialized")
	}
	raw, err := hex.DecodeString(hexCipher)
	if err != nil {
		return "", err
	}
	if len(raw) < aes.BlockSize+aes.BlockSize {
		return "", errors.New("ciphertext too short")
	}
	iv := raw[:aes.BlockSize]
	ciphertext := raw[aes.BlockSize:]
	if len(ciphertext)%aes.BlockSize != 0 {
		return "", errors.New("invalid ciphertext length")
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", err
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	out, err := pkcs7Unpad(plaintext, aes.BlockSize)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("invalid padding")
	}
	padLen := int(data[len(data)-1])
	if padLen == 0 || padLen > blockSize {
		return nil, errors.New("invalid padding")
	}
	for i := 0; i < padLen; i++ {
		if data[len(data)-1-i] != byte(padLen) {
			return nil, errors.New("invalid padding")
		}
	}
	return data[:len(data)-padLen], nil
}

func Decrypt(ciphertextHex string) (string, error) {
	if ciphertextHex == "" {
		return "", nil
	}
	ciphertext, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := aesGCM.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
