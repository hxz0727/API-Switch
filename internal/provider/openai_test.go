package provider

import (
	"strings"
	"testing"
)

func TestOpenAIEndpoint_Basic(t *testing.T) {
	result := openAIEndpoint("https://api.openai.com")
	if result != "https://api.openai.com/v1/chat/completions" {
		t.Errorf("expected 'https://api.openai.com/v1/chat/completions', got %q", result)
	}
}

func TestOpenAIEndpoint_WithV1(t *testing.T) {
	result := openAIEndpoint("https://api.openai.com/v1")
	if result != "https://api.openai.com/v1/chat/completions" {
		t.Errorf("expected 'https://api.openai.com/v1/chat/completions', got %q", result)
	}
}

func TestOpenAIEndpoint_WithV1TrailingSlash(t *testing.T) {
	result := openAIEndpoint("https://api.openai.com/v1/")
	if result != "https://api.openai.com/v1/chat/completions" {
		t.Errorf("expected 'https://api.openai.com/v1/chat/completions', got %q", result)
	}
}

func TestOpenAIEndpoint_AlreadyComplete(t *testing.T) {
	result := openAIEndpoint("https://api.openai.com/v1/chat/completions")
	if result != "https://api.openai.com/v1/chat/completions" {
		t.Errorf("expected unchanged URL, got %q", result)
	}
}

func TestOpenAIEndpoint_TrailingSlash(t *testing.T) {
	result := openAIEndpoint("https://api.deepseek.com/")
	if result != "https://api.deepseek.com/v1/chat/completions" {
		t.Errorf("expected 'https://api.deepseek.com/v1/chat/completions', got %q", result)
	}
}

func TestOpenAIEndpoint_WithPath(t *testing.T) {
	result := openAIEndpoint("https://api.moonshot.cn")
	if result != "https://api.moonshot.cn/v1/chat/completions" {
		t.Errorf("expected 'https://api.moonshot.cn/v1/chat/completions', got %q", result)
	}
}

func TestOpenAIModelsEndpoint_Basic(t *testing.T) {
	result := openAIModelsEndpoint("https://api.openai.com")
	if result != "https://api.openai.com/models" {
		t.Errorf("expected 'https://api.openai.com/models', got %q", result)
	}
}

func TestOpenAIModelsEndpoint_WithV1(t *testing.T) {
	result := openAIModelsEndpoint("https://api.openai.com/v1")
	if result != "https://api.openai.com/v1/models" {
		t.Errorf("expected 'https://api.openai.com/v1/models', got %q", result)
	}
}

func TestOpenAIModelsEndpoint_WithV1TrailingSlash(t *testing.T) {
	result := openAIModelsEndpoint("https://api.openai.com/v1/")
	if result != "https://api.openai.com/v1/models" {
		t.Errorf("expected 'https://api.openai.com/v1/models', got %q", result)
	}
}

func TestOpenAIModelsEndpoint_WithCompletions(t *testing.T) {
	result := openAIModelsEndpoint("https://api.openai.com/v1/chat/completions")
	if result != "https://api.openai.com/v1/models" {
		t.Errorf("expected 'https://api.openai.com/v1/models', got %q", result)
	}
}

func TestTruncateBody_Short(t *testing.T) {
	result := truncateBody("short message", 500)
	if result != "short message" {
		t.Errorf("expected 'short message', got %q", result)
	}
}

func TestTruncateBody_Long(t *testing.T) {
	longStr := strings.Repeat("x", 1000)
	result := truncateBody(longStr, 500)
	if len(result) != 500+len("...(truncated)") {
		t.Errorf("expected length %d, got %d", 500+len("...(truncated)"), len(result))
	}
	if !strings.HasSuffix(result, "...(truncated)") {
		t.Error("expected truncated suffix")
	}
}

func TestTruncateBody_Exact(t *testing.T) {
	exactStr := strings.Repeat("x", 500)
	result := truncateBody(exactStr, 500)
	if result != exactStr {
		t.Errorf("expected exact string, got %q", result)
	}
}
