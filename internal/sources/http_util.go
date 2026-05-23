package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultHTTPTimeout = 15 * time.Second

func httpGet(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	return httpGetTimeout(ctx, url, headers, defaultHTTPTimeout)
}

func httpGetTimeout(ctx context.Context, url string, headers map[string]string, timeout time.Duration) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	client := &http.Client{Timeout: timeout}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

func httpPost(ctx context.Context, url string, headers map[string]string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "text/plain")
	}
	client := &http.Client{Timeout: defaultHTTPTimeout}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, truncate(string(out), 200))
	}
	return out, nil
}

func parseCHRows(body []byte) ([]CHRow, error) {
	var direct []CHRow
	if err := json.Unmarshal(body, &direct); err == nil && len(direct) > 0 {
		return direct, nil
	}
	var wrapped struct {
		Data []CHRow `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && len(wrapped.Data) > 0 {
		return wrapped.Data, nil
	}
	var nested struct {
		Data struct {
			Data []CHRow `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &nested); err == nil && len(nested.Data.Data) > 0 {
		return nested.Data.Data, nil
	}
	return nil, fmt.Errorf("unexpected ClickHouse response shape")
}

func parseCapabilityNames(body []byte) []string {
	var direct []struct {
		CapabilityName string `json:"capability_name"`
	}
	if err := json.Unmarshal(body, &direct); err == nil && len(direct) > 0 {
		out := make([]string, 0, len(direct))
		for _, r := range direct {
			if r.CapabilityName != "" {
				out = append(out, r.CapabilityName)
			}
		}
		return out
	}
	var wrapped struct {
		Data []struct {
			CapabilityName string `json:"capability_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil {
		out := make([]string, 0, len(wrapped.Data))
		for _, r := range wrapped.Data {
			if r.CapabilityName != "" {
				out = append(out, r.CapabilityName)
			}
		}
		return out
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
