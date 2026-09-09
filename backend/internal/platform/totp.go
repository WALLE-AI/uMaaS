package platform

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP（RFC 6238）。admin 全员强制 2FA（ARCHITECTURE.md §4.5）。
//
// 自己实现而不引第三方库：算法本身是二十行，而这条链路上的每一个依赖
// 都能读到 2FA 密钥。依赖面越小越好。

const (
	totpPeriod  = 30 // 秒
	totpDigits  = 6
	totpSkew    = 1 // 允许前后各一个时间窗，覆盖时钟漂移
	totpSecretB = 20
)

// NewTOTPSecret 生成一个 base32 编码的 TOTP 密钥。
func NewTOTPSecret() (string, error) {
	buf := make([]byte, totpSecretB)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

// TOTPURI 生成可被认证器扫描的 otpauth:// URI。
func TOTPURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// TOTPCode 计算某个时刻的验证码。
func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).
		DecodeString(strings.ToUpper(strings.ReplaceAll(secret, " ", "")))
	if err != nil {
		return "", fmt.Errorf("decode secret: %w", err)
	}

	counter := uint64(t.Unix()) / totpPeriod
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	mod := uint32(1)
	for range totpDigits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, value%mod), nil
}

// VerifyTOTP 校验验证码，允许 ±1 个时间窗的时钟漂移。
//
// 用常数时间比较，避免通过响应时间逐位试探。
func VerifyTOTP(secret, code string, now time.Time) (bool, error) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false, nil
	}
	for skew := -totpSkew; skew <= totpSkew; skew++ {
		want, err := TOTPCode(secret, now.Add(time.Duration(skew*totpPeriod)*time.Second))
		if err != nil {
			return false, err
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true, nil
		}
	}
	return false, nil
}
