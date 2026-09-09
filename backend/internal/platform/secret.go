package platform

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// ── 会话令牌 ────────────────────────────────────────────────────

// SessionTokenBytes 是会话令牌的熵。32 字节 = 256 bit，
// 足以让暴力猜测在任何现实算力下不成立。
const SessionTokenBytes = 32

// NewSessionToken 生成一个会话令牌，返回明文（发给客户端）与哈希（入库）。
//
// 数据库里**只存哈希**：库泄露不等于会话被劫持（ARCHITECTURE.md §5.3）。
// 用 SHA-256 而不是 argon2——令牌本身已经是 256 bit 均匀随机，
// 不存在字典攻击的可能，慢哈希在这里只会拖慢每一个请求的会话校验。
func NewSessionToken() (plain string, hash []byte, err error) {
	buf := make([]byte, SessionTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, fmt.Errorf("read random: %w", err)
	}
	plain = base64.RawURLEncoding.EncodeToString(buf)
	return plain, HashSessionToken(plain), nil
}

// HashSessionToken 计算令牌哈希。
func HashSessionToken(plain string) []byte {
	sum := sha256.Sum256([]byte(plain))
	return sum[:]
}

// ── 密码 ────────────────────────────────────────────────────────

// argon2id 参数。OWASP 2024 建议的下限之上取值：
// 19 MiB 内存、2 次迭代、并行度 1。
const (
	argonTime    = 2
	argonMemory  = 19 * 1024 // KiB
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

// HashPassword 用 argon2id 哈希密码，返回自描述的编码串。
//
// 编码里带参数，是为了将来调高强度时**老密码仍能校验**——
// 参数写死在代码里的方案，调参那天等于让所有人无法登录。
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword 校验密码。
//
// 用常数时间比较：普通的 == 会因为提前返回而泄露前缀匹配长度。
func VerifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, fmt.Errorf("unsupported password hash format")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, fmt.Errorf("parse version: %w", err)
	}
	var memory uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, fmt.Errorf("parse params: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("decode salt: %w", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("decode key: %w", err)
	}

	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// DummyPasswordHash 是一个固定的合法哈希，用于"用户不存在"时仍走一遍校验。
//
// 不这么做，登录接口的响应时间会泄露账号是否存在——
// 存在时慢（跑 argon2），不存在时快（直接返回）。这是可被脚本利用的枚举通道。
var DummyPasswordHash = "$argon2id$v=19$m=19456,t=2,p=1$" +
	"YWJjZGVmZ2hpamtsbW5vcA$" + // 固定 salt，无所谓：它保护的不是任何真实密码
	"7NDPnbLQFxLxvVZ6QG0oWkq8VJmqBWMGjgpQqfBFYVc"

// HexHash 供日志与调试用的短摘要。绝不用于安全判断。
func HexHash(b []byte) string { return hex.EncodeToString(b)[:12] }
