package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type LLM struct {
	Provider, Key, Model, Base string
	Client                     *http.Client
}

func newLLM() *LLM {
	provider := env("LLM_PROVIDER", "openai")
	prefix := strings.ToUpper(provider)
	bases := map[string]string{"openai": "https://api.openai.com/v1", "anthropic": "https://api.anthropic.com/v1", "gemini": "https://generativelanguage.googleapis.com/v1beta", "ollama": "http://localhost:11434"}
	models := map[string]string{"openai": "gpt-4o", "anthropic": "claude-3-5-sonnet-20241022", "gemini": "gemini-1.5-pro", "ollama": "llama3.1"}
	return &LLM{provider, osEnv(prefix + "_API_KEY"), env(prefix+"_MODEL", models[provider]), env(prefix+"_BASE_URL", bases[provider]), &http.Client{Timeout: 110 * time.Second}}
}

// Kept separate to make provider configuration explicit and testable.
func osEnv(key string) string { return env(key, "") }
func parseModelJSON(raw string) (Obj, error) {
	raw = strings.TrimSpace(raw)
	var out Obj
	if e := json.Unmarshal([]byte(raw), &out); e == nil && out != nil {
		return out, nil
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("model did not return JSON")
	}
	if e := json.Unmarshal([]byte(raw[start:end+1]), &out); e != nil {
		return nil, fmt.Errorf("model returned malformed JSON")
	}
	return out, nil
}
func (l *LLM) Complete(ctx context.Context, prompt, system string, temp float64, maxTokens int) (Obj, error) {
	if l.Provider != "ollama" && l.Key == "" {
		return nil, httpError{503, "尚未配置模型服务凭据"}
	}
	endpoint := strings.TrimRight(l.Base, "/")
	headers := map[string]string{"Content-Type": "application/json"}
	var payload Obj
	messages := []Obj{{"role": "system", "content": system}, {"role": "user", "content": prompt}}
	switch l.Provider {
	case "openai":
		endpoint += "/chat/completions"
		headers["Authorization"] = "Bearer " + l.Key
		payload = Obj{"model": l.Model, "messages": messages, "temperature": temp, "max_tokens": maxTokens}
	case "anthropic":
		endpoint += "/messages"
		headers["x-api-key"] = l.Key
		headers["anthropic-version"] = "2023-06-01"
		payload = Obj{"model": l.Model, "system": system, "messages": []Obj{{"role": "user", "content": prompt}}, "temperature": temp, "max_tokens": maxTokens}
	case "gemini":
		endpoint += "/models/" + url.PathEscape(l.Model) + ":generateContent"
		headers["x-goog-api-key"] = l.Key
		payload = Obj{"systemInstruction": Obj{"parts": []Obj{{"text": system}}}, "contents": []Obj{{"role": "user", "parts": []Obj{{"text": prompt}}}}, "generationConfig": Obj{"temperature": temp, "maxOutputTokens": maxTokens}}
	case "ollama":
		endpoint += "/api/chat"
		payload = Obj{"model": l.Model, "messages": messages, "stream": false, "options": Obj{"temperature": temp, "num_predict": maxTokens}}
	default:
		return nil, httpError{503, "不支持的模型服务配置"}
	}
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBytes(payload)))
	if e != nil {
		return nil, e
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, e := l.Client.Do(req)
	if e != nil {
		return nil, e
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return nil, fmt.Errorf("model provider returned HTTP %d", response.StatusCode)
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
	if e != nil {
		return nil, e
	}
	if len(raw) > 4*1024*1024 {
		return nil, fmt.Errorf("model response too large")
	}
	var content string
	switch l.Provider {
	case "openai":
		var v struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if e = json.Unmarshal(raw, &v); e != nil {
			return nil, e
		}
		if len(v.Choices) > 0 {
			content = v.Choices[0].Message.Content
		}
	case "anthropic":
		var v struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if e = json.Unmarshal(raw, &v); e != nil {
			return nil, e
		}
		for _, part := range v.Content {
			content += part.Text
		}
	case "gemini":
		var v struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if e = json.Unmarshal(raw, &v); e != nil {
			return nil, e
		}
		if len(v.Candidates) > 0 {
			for _, part := range v.Candidates[0].Content.Parts {
				content += part.Text
			}
		}
	case "ollama":
		var v struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if e = json.Unmarshal(raw, &v); e != nil {
			return nil, e
		}
		content = v.Message.Content
	}
	return parseModelJSON(content)
}
