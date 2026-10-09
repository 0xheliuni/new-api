package common

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unsafe"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"

	"github.com/samber/lo"
)

const LocalLogContentLimit = 2048

// LocalLogPreview limits log-only content unless debug logging is enabled.
func LocalLogPreview(content string) string {
	if DebugEnabled || len(content) <= LocalLogContentLimit {
		return content
	}
	return fmt.Sprintf("%s... [truncated, original_length=%d, limit=%d]", content[:LocalLogContentLimit], len(content), LocalLogContentLimit)
}

func GetStringIfEmpty(str string, defaultValue string) string {
	if str == "" {
		return defaultValue
	}
	return str
}

func GetRandomString(length int) string {
	if length <= 0 {
		return ""
	}
	return lo.RandomString(length, lo.AlphanumericCharset)
}

// GetRandomStringWithCharset returns a length-N random string drawn from the
// requested character set. Recognized charsets:
//
//   - "alphanumeric" (default): A-Z, a-z, 0-9
//   - "digits": 0-9
//   - "letters": A-Z, a-z
//
// Any other charset value (including the empty string) falls back to alphanumeric,
// which is the safest superset and matches the legacy GetRandomString contract.
// A non-positive length returns "" without invoking the underlying RNG.
//
// Used by the auto-create-user feature to honor admin-configurable username
// suffix charsets; the alphanumeric branch is also the random-password charset.
func GetRandomStringWithCharset(length int, charset string) string {
	if length <= 0 {
		return ""
	}
	switch charset {
	case "digits":
		return lo.RandomString(length, lo.NumbersCharset)
	case "letters":
		return lo.RandomString(length, lo.LettersCharset)
	default:
		return lo.RandomString(length, lo.AlphanumericCharset)
	}
}

func MapToJsonStr(m map[string]any) string {
	bytes, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func StrToMap(str string) (map[string]any, error) {
	m := make(map[string]any)
	err := Unmarshal([]byte(str), &m)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func StrToJsonArray(str string) ([]any, error) {
	var js []any
	err := json.Unmarshal([]byte(str), &js)
	if err != nil {
		return nil, err
	}
	return js, nil
}

func IsJsonArray(str string) bool {
	var js []any
	return json.Unmarshal([]byte(str), &js) == nil
}

func IsJsonObject(str string) bool {
	var js map[string]any
	return json.Unmarshal([]byte(str), &js) == nil
}

func String2Int(str string) int {
	num, err := strconv.Atoi(str)
	if err != nil {
		return 0
	}
	return num
}

func StringsContains(strs []string, str string) bool {
	return slices.Contains(strs, str)
}

// StringToByteSlice []byte only read, panic on append
func StringToByteSlice(s string) []byte {
	tmp1 := (*[2]uintptr)(unsafe.Pointer(&s))
	tmp2 := [3]uintptr{tmp1[0], tmp1[1], tmp1[1]}
	return *(*[]byte)(unsafe.Pointer(&tmp2))
}

func EncodeBase64(str string) string {
	return base64.StdEncoding.EncodeToString([]byte(str))
}

func GetJsonString(data any) string {
	if data == nil {
		return ""
	}
	b, _ := json.Marshal(data)
	return string(b)
}

// NormalizeBillingPreference clamps the billing preference to valid values.
func NormalizeBillingPreference(pref string) string {
	switch strings.TrimSpace(pref) {
	case "subscription_first", "wallet_first", "subscription_only", "wallet_only":
		return strings.TrimSpace(pref)
	default:
		return "subscription_first"
	}
}

// MaskEmail masks a user email to prevent PII leakage in logs
// Returns "***masked***" if email is empty, otherwise shows only the domain part
func MaskEmail(email string) string {
	if email == "" {
		return "***masked***"
	}

	// Find the @ symbol
	_, after, ok := strings.Cut(email, "@")
	if !ok {
		// No @ symbol found, return masked
		return "***masked***"
	}

	// Return only the domain part with @ symbol
	return "***@" + after
}

// MaskSensitiveInfo moved to the conversion kit (kitutil) because the types
// package error formatting depends on it; host callers keep this name.
func MaskSensitiveInfo(str string) string {
	return kitutil.MaskSensitiveInfo(str)
}

// upstreamRequestIdRe 匹配上游报错文本中的 "Request ID: xxx"（大小写变体均可）。
var upstreamRequestIdRe = regexp.MustCompile(`(?i)(request\s*id:?\s*)([A-Za-z0-9_\-]+)`)

// ReplaceUpstreamRequestId 把上游报错里的 Request ID 替换为我们系统的 request id：
// 客户排查凭据应是系统 ID（可在日志按 request_id 检索），上游内部 ID 对客户无用
// 且泄露上游细节。rid 为空时保留原文。
func ReplaceUpstreamRequestId(msg, rid string) string {
	if rid == "" || msg == "" {
		return msg
	}
	return upstreamRequestIdRe.ReplaceAllString(msg, "${1}"+rid)
}
