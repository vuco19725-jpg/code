package utils

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestResp(t *testing.T) {
	resp := Resp(0, "success", nil)

	if resp.Code != 0 {
		t.Errorf("Resp() Code = %d, want 0", resp.Code)
	}
	if resp.Msg != "success" {
		t.Errorf("Resp() Msg = %s, want 'success'", resp.Msg)
	}
	if resp.Data != nil {
		t.Errorf("Resp() Data = %v, want nil", resp.Data)
	}
}

func TestResp_WithData(t *testing.T) {
	data := map[string]interface{}{"id": 1, "name": "test"}
	resp := Resp(0, "success", data)

	if resp.Code != 0 {
		t.Errorf("Resp() Code = %d, want 0", resp.Code)
	}
	if resp.Msg != "success" {
		t.Errorf("Resp() Msg = %s, want 'success'", resp.Msg)
	}
	if resp.Data == nil {
		t.Error("Resp() Data = nil, want non-nil")
	}

	// 验证 JSON 序列化
	jsonData, err := json.Marshal(resp)
	if err != nil {
		t.Errorf("json.Marshal() error = %v", err)
	}

	var decoded map[string]interface{}
	json.Unmarshal(jsonData, &decoded)

	if decoded["code"].(float64) != 0 {
		t.Errorf("JSON code = %v, want 0", decoded["code"])
	}
	if decoded["msg"].(string) != "success" {
		t.Errorf("JSON msg = %v, want 'success'", decoded["msg"])
	}
}

func TestResp_ErrorCode(t *testing.T) {
	tests := []struct {
		code int
		msg  string
	}{
		{1001, "参数错误"},
		{2001, "商品不存在"},
		{4001, "请求过于频繁"},
		{5001, "系统错误"},
	}

	for _, tt := range tests {
		resp := Resp(tt.code, tt.msg, nil)
		if resp.Code != tt.code {
			t.Errorf("Resp() Code = %d, want %d", resp.Code, tt.code)
		}
		if resp.Msg != tt.msg {
			t.Errorf("Resp() Msg = %s, want %s", resp.Msg, tt.msg)
		}
	}
}

func TestSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/test", nil)

	data := map[string]interface{}{"id": 123}
	Success(c, data)

	if w.Code != http.StatusOK {
		t.Errorf("Success() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if resp.Code != 0 {
		t.Errorf("Success() response Code = %d, want 0", resp.Code)
	}
	if resp.Msg != "success" {
		t.Errorf("Success() response Msg = %s, want 'success'", resp.Msg)
	}
}

func TestSuccess_NilData(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/test", nil)

	Success(c, nil)

	if w.Code != http.StatusOK {
		t.Errorf("Success() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if resp.Code != 0 {
		t.Errorf("Success() response Code = %d, want 0", resp.Code)
	}
	if resp.Data != nil {
		t.Errorf("Success() response Data = %v, want nil", resp.Data)
	}
}

func TestErrorResp(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/test", nil)

	ErrorResp(c, 4001, "请求过于频繁")

	if w.Code != http.StatusOK {
		t.Errorf("ErrorResp() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if resp.Code != 4001 {
		t.Errorf("ErrorResp() response Code = %d, want 4001", resp.Code)
	}
	if resp.Msg != "请求过于频繁" {
		t.Errorf("ErrorResp() response Msg = %s, want '请求过于频繁'", resp.Msg)
	}
	if resp.Data != nil {
		t.Errorf("ErrorResp() response Data = %v, want nil", resp.Data)
	}
}

func TestErrorResp_DifferentCodes(t *testing.T) {
	tests := []struct {
		code    int
		msg     string
	}{
		{1001, "参数错误"},
		{2001, "商品不存在"},
		{2004, "库存不足"},
		{5001, "系统错误"},
		{5002, "系统繁忙"},
	}

	for _, tt := range tests {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/test", nil)

		ErrorResp(c, tt.code, tt.msg)

		var resp Response
		json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Code != tt.code {
			t.Errorf("ErrorResp() Code = %d, want %d", resp.Code, tt.code)
		}
		if resp.Msg != tt.msg {
			t.Errorf("ErrorResp() Msg = %s, want %s", resp.Msg, tt.msg)
		}
	}
}

func TestResponse_JSONTags(t *testing.T) {
	resp := Resp(0, "success", "test data") // 使用非 nil data 验证字段存在

	// 验证 JSON 标签
	jsonData, _ := json.Marshal(resp)
	jsonStr := string(jsonData)

	// 检查是否包含正确的 JSON 标签
	if !contains(jsonStr, `"code"`) {
		t.Error("Response JSON should contain 'code' field")
	}
	if !contains(jsonStr, `"msg"`) {
		t.Error("Response JSON should contain 'msg' field")
	}
	if !contains(jsonStr, `"data"`) {
		t.Error("Response JSON should contain 'data' field")
	}
}

func TestResponse_OmitsEmptyData(t *testing.T) {
	// 当 data 为 nil 时，JSON 中不应该包含 data 字段或应该为 null
	resp := Resp(0, "success", nil)
	jsonData, _ := json.Marshal(resp)

	var decoded map[string]json.RawMessage
	json.Unmarshal(jsonData, &decoded)

	// data 字段应该不存在或者为 null
	if data, ok := decoded["data"]; ok {
		if string(data) != "null" {
			t.Errorf("Response JSON data should be omitted or null when nil, got %s", string(data))
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func BenchmarkResp(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Resp(0, "success", nil)
	}
}

func BenchmarkResp_WithData(b *testing.B) {
	data := map[string]interface{}{"id": 1, "name": "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Resp(0, "success", data)
	}
}
