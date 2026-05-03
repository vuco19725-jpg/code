package utils

import (
	"testing"
	"time"
)

func TestMaskPhone(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"13812345678", "138****5678"},
		{"15900001111", "159****1111"},
		{"18612345678", "186****5678"},
		{"12345678901", "123****8901"},
		{"short", "short"},           // 长度不足
		{"", ""},                     // 空字符串
		{"12345", "12345"},           // 长度不足 11 位
	}

	for _, tt := range tests {
		result := MaskPhone(tt.input)
		if result != tt.expected {
			t.Errorf("MaskPhone(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestMaskPhone_InvalidLength(t *testing.T) {
	// 测试各种无效长度
	invalidLengths := []string{"", "1", "12", "123", "1234", "12345", "123456", "1234567", "12345678", "1234567890", "123456789012"}

	for _, input := range invalidLengths {
		result := MaskPhone(input)
		if result != input {
			t.Errorf("MaskPhone(%q) should return input unchanged for invalid length, got %q", input, result)
		}
	}
}

func TestMaskEmail(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"test@example.com", "t***@example.com"},
		{"user@domain.com", "u***@domain.com"},
		{"admin@company.org", "a***@company.org"},
		{"a@n.com", "***@n.com"},         // 用户名只有 1 位
		{"ab@c.com", "***@c.com"},        // 用户名只有 2 位
		{"test", "***"},                  // 无 @ 符号
		{"", "***"},                      // 空字符串
		{"@", "***@"},                    // 只有 @
		{"noat", "***"},                  // 没有 @
	}

	for _, tt := range tests {
		result := MaskEmail(tt.input)
		if result != tt.expected {
			t.Errorf("MaskEmail(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestMaskEmail_SpecialCases(t *testing.T) {
	// 测试各种边界情况
	tests := []struct {
		input    string
		expected string
	}{
		{"a@b", "***@b"},                              // 最短有效邮箱
		{"ab@bc", "***@bc"},                          // 用户名只有 2 位，保留首字符
		{"verylongemail@domain.com", "v***@domain.com"}, // 长邮箱
	}

	for _, tt := range tests {
		result := MaskEmail(tt.input)
		if result != tt.expected {
			t.Errorf("MaskEmail(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestMaskIDCard(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"110101199001011234", "1101**********1234"},
		{"310000199001011234", "3100**********1234"},
		{"12345678", "1234**********5678"},          // 8位也脱敏
		{"", "***"},                                 // 空字符串
		{"123", "***"},                              // 太短
		{"1234567", "***"},                          // 仍然太短
		{"123456789012345678", "1234**********5678"}, // 18 位
	}

	for _, tt := range tests {
		result := MaskIDCard(tt.input)
		if result != tt.expected {
			t.Errorf("MaskIDCard(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestMaskIDCard_Exactly8Chars(t *testing.T) {
	result := MaskIDCard("12345678")
	if result != "1234**********5678" {
		t.Errorf("MaskIDCard(\"12345678\") = %q, want \"1234**********5678\"", result)
	}
}

func TestMaskIDCard_MinimumLength(t *testing.T) {
	for i := 0; i < 8; i++ {
		input := ""
		for j := 0; j < i; j++ {
			input += "1"
		}
		result := MaskIDCard(input)
		if result != "***" {
			t.Errorf("MaskIDCard(%q) = %q, want \"***\" for length %d", input, result, i)
		}
	}
}

func TestString(t *testing.T) {
	field := String("key", "value")
	if field.Key != "key" {
		t.Errorf("String().Key = %q, want \"key\"", field.Key)
	}
}

func TestInt(t *testing.T) {
	field := Int("count", 42)
	if field.Key != "count" {
		t.Errorf("Int().Key = %q, want \"count\"", field.Key)
	}
}

func TestInt64(t *testing.T) {
	field := Int64("big", 1234567890)
	if field.Key != "big" {
		t.Errorf("Int64().Key = %q, want \"big\"", field.Key)
	}
}

func TestUint(t *testing.T) {
	field := Uint("unsigned", 100)
	if field.Key != "unsigned" {
		t.Errorf("Uint().Key = %q, want \"unsigned\"", field.Key)
	}
}

func TestErr(t *testing.T) {
	field := Err(nil)
	if field.Key != "" {
		t.Errorf("Err(nil).Key = %q, want \"\"", field.Key)
	}
}

func TestAny(t *testing.T) {
	field := Any("any", map[string]int{"a": 1})
	if field.Key != "any" {
		t.Errorf("Any().Key = %q, want \"any\"", field.Key)
	}
}

func TestBool(t *testing.T) {
	field := Bool("flag", true)
	if field.Key != "flag" {
		t.Errorf("Bool().Key = %q, want \"flag\"", field.Key)
	}
}

func TestDuration(t *testing.T) {
	field := Duration("elapsed", 0)
	if field.Key != "elapsed" {
		t.Errorf("Duration().Key = %q, want \"elapsed\"", field.Key)
	}
}

func TestTime(t *testing.T) {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	field := Time("created", testTime)
	if field.Key != "created" {
		t.Errorf("Time().Key = %q, want \"created\"", field.Key)
	}
}

func TestSensitiveField(t *testing.T) {
	field := SensitiveField("password", "secret123")
	if field.Key != "password" {
		t.Errorf("SensitiveField().Key = %q, want \"password\"", field.Key)
	}
}

func TestStringS(t *testing.T) {
	field := StringS("key", "value")
	if field.Key != "key" {
		t.Errorf("StringS().Key = %q, want \"key\"", field.Key)
	}
}
