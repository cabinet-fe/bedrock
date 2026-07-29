import { cbc, gcm } from "@noble/ciphers/aes.js";
import { bytesToHex, bytesToUtf8, hexToBytes, utf8ToBytes } from "@noble/ciphers/utils.js";

/** AES-GCM nonce size used by Go cipher.NewGCM (internal/pkg.Encrypt). */
const AES_GCM_NONCE_SIZE = 12;

function isValidHexKey64(s: string): boolean {
  return /^[0-9a-fA-F]{64}$/.test(s);
}

function randomIV(): Uint8Array {
  const iv = new Uint8Array(16);
  crypto.getRandomValues(iv);
  return iv;
}

/** Secure context (HTTPS, localhost) → Web Crypto; otherwise @noble/ciphers. */
function useSubtle(): boolean {
  return typeof crypto !== "undefined" && typeof crypto.subtle !== "undefined";
}

function getEncryptionKeyHex(): string {
  if (typeof window !== "undefined") {
    const injected = window.__BEDROCK_ENCRYPTION_KEY__?.trim() ?? "";
    if (isValidHexKey64(injected)) {
      return injected;
    }
  }
  const fromEnv = import.meta.env.VITE_BEDROCK_ENCRYPTION_KEY?.trim() ?? "";
  if (isValidHexKey64(fromEnv)) {
    return fromEnv;
  }
  throw new Error(
    "需要有效的加密密钥（64 位 hex，与后端 encryption.key 一致）：嵌入部署由服务端注入 window.__BEDROCK_ENCRYPTION_KEY__，本地开发可设 VITE_BEDROCK_ENCRYPTION_KEY",
  );
}

function getEncryptionKeyBytes(): Uint8Array {
  const keyBytes = hexToBytes(getEncryptionKeyHex());
  if (keyBytes.length !== 32) {
    throw new Error("加密密钥长度应为 32 字节（64 hex 字符）");
  }
  return keyBytes;
}

async function encryptSubtleCBC(plain: string, keyBytes: Uint8Array): Promise<string> {
  const iv = randomIV();
  const key = await crypto.subtle.importKey(
    "raw",
    keyBytes.buffer as ArrayBuffer,
    "AES-CBC",
    false,
    ["encrypt"],
  );
  const ciphertext = await crypto.subtle.encrypt(
    { name: "AES-CBC", iv: iv.buffer as ArrayBuffer },
    key,
    new TextEncoder().encode(plain),
  );
  const ct = new Uint8Array(ciphertext);
  const combined = new Uint8Array(iv.length + ct.length);
  combined.set(iv, 0);
  combined.set(ct, iv.length);
  return bytesToHex(combined);
}

function encryptNobleCBC(plain: string, keyBytes: Uint8Array): string {
  const iv = randomIV();
  const ciphertext = cbc(keyBytes, iv).encrypt(utf8ToBytes(plain));
  const combined = new Uint8Array(iv.length + ciphertext.length);
  combined.set(iv, 0);
  combined.set(ciphertext, iv.length);
  return bytesToHex(combined);
}

/**
 * AES-256-CBC → hex(IV(16) || PKCS#7 ciphertext). Never falls back to plaintext password.
 */
export async function encryptLoginPassword(plain: string): Promise<string> {
  const keyBytes = getEncryptionKeyBytes();
  if (useSubtle()) {
    return encryptSubtleCBC(plain, keyBytes);
  }
  return encryptNobleCBC(plain, keyBytes);
}

function asArrayBuffer(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer;
}

async function decryptSubtleGCM(raw: Uint8Array, keyBytes: Uint8Array): Promise<string> {
  const nonce = raw.slice(0, AES_GCM_NONCE_SIZE);
  const data = raw.slice(AES_GCM_NONCE_SIZE);
  const key = await crypto.subtle.importKey("raw", asArrayBuffer(keyBytes), "AES-GCM", false, [
    "decrypt",
  ]);
  const plaintext = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: asArrayBuffer(nonce) },
    key,
    asArrayBuffer(data),
  );
  return new TextDecoder().decode(plaintext);
}

function decryptNobleGCM(raw: Uint8Array, keyBytes: Uint8Array): string {
  const nonce = raw.slice(0, AES_GCM_NONCE_SIZE);
  const data = raw.slice(AES_GCM_NONCE_SIZE);
  return bytesToUtf8(gcm(keyBytes, nonce).decrypt(data));
}

/**
 * Decrypts storage ciphertext from Go pkg.Encrypt: hex(nonce(12) || ciphertext||tag).
 * Uses the same 32-byte key as login (window.__BEDROCK_ENCRYPTION_KEY__ / VITE_BEDROCK_ENCRYPTION_KEY).
 */
export async function decryptAESGCM(ciphertextHex: string): Promise<string> {
  const hex = ciphertextHex.trim();
  if (!hex) {
    throw new Error("密文为空");
  }
  const raw = hexToBytes(hex);
  if (raw.length <= AES_GCM_NONCE_SIZE) {
    throw new Error("密文过短");
  }
  const keyBytes = getEncryptionKeyBytes();
  if (useSubtle()) {
    return decryptSubtleGCM(raw, keyBytes);
  }
  return decryptNobleGCM(raw, keyBytes);
}
