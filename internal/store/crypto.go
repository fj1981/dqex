package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"os/user"
	"strings"

	"dqex/internal/engine"
)

// encPrefix 密文前缀：标识加密字段，同时用于识别旧版明文（无此头即明文，直接兼容）。
const encPrefix = "dqex-enc-v1:"

// machineKey 由本机特征（主机名 + 用户名 + uid）派生 AES-256 密钥。
// 数据被拷贝到其他机器后无法还原密钥，从而防止敏感信息泄露。
func machineKey() ([]byte, error) {
	host, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	uname := ""
	if u, err := user.Current(); err == nil {
		uname = u.Username
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("dqex|%s|%s|%d", host, uname, os.Getuid())))
	return sum[:], nil
}

// encryptString 列级敏感字段加密，输出格式：前缀 + base64(nonce || 密文)。
func encryptString(plain string) (string, error) {
	key, err := machineKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return encPrefix + base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// decryptString 解密 encryptString 的输出；空串原样返回（无密码场景）。
func decryptString(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if !strings.HasPrefix(s, encPrefix) {
		// 无前缀视为明文（兼容），直接返回
		return s, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, encPrefix))
	if err != nil {
		return "", engine.NewMsgErrf(engine.ErrCryptoFormat, err)
	}
	key, err := machineKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", engine.NewMsgErr(engine.ErrCryptoLen)
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", engine.NewMsgErr(engine.ErrCryptoDecrypt)
	}
	return string(plain), nil
}
