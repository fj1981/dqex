package service

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

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
)

// encPrefix 密文前缀：标识加密文件，同时用于识别旧版明文 JSON（无此头即明文，直接兼容）
const encPrefix = "dbimpex-enc-v1:"

// sensitiveFiles 需加密持久化的文件（含数据库密码等敏感信息）；
// history.json 无敏感信息，保持明文便于排查
var sensitiveFiles = map[string]bool{
	"connections.json": true,
	"tasks.json":       true,
}

// machineKey 由本机特征（主机名 + 用户名 + uid）派生 AES-256 密钥。
// 文件被拷贝到其他机器后无法还原密钥，从而防止配置泄露。
func machineKey() ([]byte, error) {
	host, err := os.Hostname()
	if err != nil {
		return nil, cygin.WrapError(err, ErrCryptoFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	uname := ""
	if u, err := user.Current(); err == nil {
		uname = u.Username
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("dbimpex|%s|%s|%d", host, uname, os.Getuid())))
	return sum[:], nil
}

// encryptData 使用 AES-256-GCM 加密，输出格式：前缀 + base64(nonce || 密文)
func encryptData(plain []byte) (string, error) {
	key, err := machineKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", cygin.WrapError(err, ErrCryptoFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", cygin.WrapError(err, ErrCryptoFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", cygin.WrapError(err, ErrCryptoFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	// nonce 与密文拼接存储，GCM 自带完整性校验
	return encPrefix + base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, plain, nil)), nil
}

// decryptData 解密 encryptData 的输出；密钥不可还原（如换了机器）时返回明确错误
func decryptData(s string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, encPrefix))
	if err != nil {
		return nil, cygin.WrapError(err, ErrCryptoFailed, cygin.WithErrPrint(), cygin.WithErrDetailf("配置文件密文格式无效"))
	}
	key, err := machineKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, cygin.WrapError(err, ErrCryptoFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, cygin.WrapError(err, ErrCryptoFailed, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
	}
	if len(raw) < gcm.NonceSize() {
		return nil, cygin.NewError(ErrCryptoFailed, cygin.WithErrPrint(), cygin.WithErrDetailf("配置文件密文长度无效"))
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return nil, cygin.NewError(ErrCryptoFailed, cygin.WithErrPrint(),
			cygin.WithErrDetailf("配置文件解密失败（可能配置文件来自其他机器，或已被篡改）"))
	}
	return plain, nil
}
