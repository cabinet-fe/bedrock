package service

import "bedrock/internal/pkg"

// encryptWebhookSecret encrypts a generated webhook secret for storage.
// Empty secrets stay empty (webhook disabled).
func encryptWebhookSecret(secret string) (string, error) {
	if secret == "" {
		return "", nil
	}
	return pkg.Encrypt(secret)
}

// decryptWebhookSecret restores a stored webhook secret for verification or
// display. Legacy plaintext rows (pre-000064, or a code/migration skew) fall
// through unchanged because GCM decryption fails on them.
func decryptWebhookSecret(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	plain, err := pkg.Decrypt(stored)
	if err == nil {
		return plain, nil
	}
	return stored, nil
}
