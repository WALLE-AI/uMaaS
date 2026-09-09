package platform

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	require.NoError(t, err)

	ok, err := VerifyPassword(hash, "correct horse battery staple")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = VerifyPassword(hash, "wrong password")
	require.NoError(t, err)
	assert.False(t, ok)
}

// 同一个密码两次哈希必须不同，否则说明 salt 没起作用——
// 那样一次拖库就能通过相同哈希批量识别用同一密码的账号。
func TestPasswordHashesAreSalted(t *testing.T) {
	a, err := HashPassword("same-password")
	require.NoError(t, err)
	b, err := HashPassword("same-password")
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
}

// 哈希串必须自描述参数，否则将来调高强度那天，所有老密码都无法校验。
func TestPasswordHashEncodesParameters(t *testing.T) {
	hash, err := HashPassword("x-very-secret")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(hash, "$argon2id$"))
	assert.Contains(t, hash, "m=19456,t=2,p=1")
}

// DummyPasswordHash 必须是**可解析**的：账号不存在时要走一遍真实校验，
// 否则登录接口的响应时间会泄露账号是否存在。
func TestDummyPasswordHashIsUsable(t *testing.T) {
	ok, err := VerifyPassword(DummyPasswordHash, "anything")
	require.NoError(t, err, "dummy hash must parse, otherwise the timing defence is skipped")
	assert.False(t, ok)
}

func TestVerifyPasswordRejectsGarbage(t *testing.T) {
	_, err := VerifyPassword("not-a-hash", "x")
	assert.Error(t, err)
}

func TestSessionTokenIsHashedNotStored(t *testing.T) {
	plain, hash, err := NewSessionToken()
	require.NoError(t, err)

	assert.NotEmpty(t, plain)
	assert.Len(t, hash, 32)
	// 明文绝不能出现在入库的字节里。
	assert.NotContains(t, string(hash), plain)
	assert.Equal(t, hash, HashSessionToken(plain))
}

func TestSessionTokensAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		tok, _, err := NewSessionToken()
		require.NoError(t, err)
		assert.False(t, seen[tok], "session tokens must not repeat")
		seen[tok] = true
	}
}

// ── TOTP ────────────────────────────────────────────────────────

func TestTOTPRoundTrip(t *testing.T) {
	secret, err := NewTOTPSecret()
	require.NoError(t, err)

	now := time.Unix(1_800_000_000, 0)
	code, err := TOTPCode(secret, now)
	require.NoError(t, err)
	assert.Len(t, code, 6)

	ok, err := VerifyTOTP(secret, code, now)
	require.NoError(t, err)
	assert.True(t, ok)
}

// 允许 ±1 个时间窗，覆盖客户端时钟漂移——不允许的话，
// 手机慢 5 秒的用户会在窗口边界随机登录失败。
func TestTOTPToleratesClockSkew(t *testing.T) {
	secret, err := NewTOTPSecret()
	require.NoError(t, err)
	now := time.Unix(1_800_000_000, 0)

	code, err := TOTPCode(secret, now.Add(-30*time.Second))
	require.NoError(t, err)
	ok, err := VerifyTOTP(secret, code, now)
	require.NoError(t, err)
	assert.True(t, ok)

	// 但两个窗口以外必须拒绝。
	code, err = TOTPCode(secret, now.Add(-120*time.Second))
	require.NoError(t, err)
	ok, err = VerifyTOTP(secret, code, now)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestTOTPRejectsMalformedCode(t *testing.T) {
	secret, err := NewTOTPSecret()
	require.NoError(t, err)

	for _, code := range []string{"", "12345", "1234567", "abcdef"} {
		ok, err := VerifyTOTP(secret, code, time.Now())
		require.NoError(t, err)
		assert.False(t, ok, "code %q must be rejected", code)
	}
}

func TestTOTPURIIsScannable(t *testing.T) {
	uri := TOTPURI("uMaaS", "admin@example.com", "JBSWY3DPEHPK3PXP")
	assert.True(t, strings.HasPrefix(uri, "otpauth://totp/"))
	assert.Contains(t, uri, "secret=JBSWY3DPEHPK3PXP")
	assert.Contains(t, uri, "issuer=uMaaS")
}

// ── 加密 ────────────────────────────────────────────────────────

func TestCipherRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	c, err := NewCipher(key)
	require.NoError(t, err)

	ct, err := c.Encrypt([]byte("JBSWY3DPEHPK3PXP"))
	require.NoError(t, err)
	assert.NotContains(t, string(ct), "JBSWY3DPEHPK3PXP")

	pt, err := c.Decrypt(ct)
	require.NoError(t, err)
	assert.Equal(t, "JBSWY3DPEHPK3PXP", string(pt))
}

// 同一明文两次加密必须不同（nonce 起作用），否则密文可比对。
func TestCipherUsesFreshNonce(t *testing.T) {
	c, err := NewCipher(make([]byte, 32))
	require.NoError(t, err)

	a, err := c.Encrypt([]byte("same"))
	require.NoError(t, err)
	b, err := c.Encrypt([]byte("same"))
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
}

func TestCipherRejectsTamperedCiphertext(t *testing.T) {
	c, err := NewCipher(make([]byte, 32))
	require.NoError(t, err)

	ct, err := c.Encrypt([]byte("secret"))
	require.NoError(t, err)
	ct[len(ct)-1] ^= 0xff

	_, err = c.Decrypt(ct)
	require.Error(t, err)
	// 不回显底层错误：GCM 的认证失败细节对攻击者有价值。
	assert.Equal(t, "decrypt failed", err.Error())
}

func TestCipherRejectsWrongKeySize(t *testing.T) {
	_, err := NewCipher(make([]byte, 16))
	assert.ErrorContains(t, err, "32 bytes")
}
