package platform

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// Cipher 是全系统的对称加密入口：TOTP 密钥、上游渠道凭证都走它
// （ARCHITECTURE.md §4.2/§4.5）。AES-GCM，密钥来自环境变量或 KMS。
//
// 只留这一个实现，是为了让"哪些东西被加密了、用的什么密钥"有唯一答案——
// 分散实现的后果是轮换密钥时漏掉一处，而那一处会在解密失败时才暴露。
type Cipher struct {
	aead cipher.AEAD
}

var ErrNoEncryptionKey = errors.New("encryption key is not configured")

// NewCipher 用 32 字节密钥构造。
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt 返回 nonce || ciphertext。
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	if c == nil {
		return nil, ErrNoEncryptionKey
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("read nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt 解开 Encrypt 的输出。
func (c *Cipher) Decrypt(payload []byte) ([]byte, error) {
	if c == nil {
		return nil, ErrNoEncryptionKey
	}
	ns := c.aead.NonceSize()
	if len(payload) < ns {
		return nil, errors.New("ciphertext too short")
	}
	plaintext, err := c.aead.Open(nil, payload[:ns], payload[ns:], nil)
	if err != nil {
		// 不回显底层错误：GCM 的认证失败信息对攻击者有价值。
		return nil, errors.New("decrypt failed")
	}
	return plaintext, nil
}
