package sources

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/sha3"
)

const serviceURIABI = "getServiceURI(address)"

func lookupServiceURI(
	ctx context.Context,
	rpcURL string,
	contract string,
	ethAddress string,
	userAgent string,
	timeout time.Duration,
) (string, error) {
	rpcURL = strings.TrimSpace(rpcURL)
	contract = strings.TrimSpace(contract)
	if rpcURL == "" || contract == "" {
		return "", nil
	}
	callData, err := serviceURICallData(ethAddress)
	if err != nil {
		return "", err
	}
	result, err := ethCall(ctx, rpcURL, contract, callData, userAgent, timeout)
	if err != nil {
		return "", err
	}
	return decodeABIString(result)
}

func ethCall(
	ctx context.Context,
	rpcURL string,
	to string,
	data string,
	userAgent string,
	timeout time.Duration,
) (string, error) {
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_call",
		"params": []any{
			map[string]string{
				"to":   to,
				"data": data,
			},
			"latest",
		},
	})

	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	res, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("RPC HTTP %d: %s", res.StatusCode, truncate(string(body), 200))
	}

	var out struct {
		Result string `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.Error != nil {
		return "", fmt.Errorf("RPC %d: %s", out.Error.Code, out.Error.Message)
	}
	return out.Result, nil
}

func serviceURICallData(ethAddress string) (string, error) {
	padded, err := paddedAddress(ethAddress)
	if err != nil {
		return "", err
	}
	return "0x" + methodSelector(serviceURIABI) + padded, nil
}

func methodSelector(signature string) string {
	hash := sha3.NewLegacyKeccak256()
	_, _ = hash.Write([]byte(signature))
	return hex.EncodeToString(hash.Sum(nil)[:4])
}

func paddedAddress(ethAddress string) (string, error) {
	addr := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ethAddress)), "0x")
	if len(addr) != 40 {
		return "", fmt.Errorf("invalid eth address %q", ethAddress)
	}
	if _, err := hex.DecodeString(addr); err != nil {
		return "", fmt.Errorf("invalid eth address %q: %w", ethAddress, err)
	}
	return strings.Repeat("0", 24) + addr, nil
}

func decodeABIString(raw string) (string, error) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "0x")
	if raw == "" {
		return "", nil
	}
	data, err := hex.DecodeString(raw)
	if err != nil {
		return "", err
	}
	if len(data) < 64 {
		return "", nil
	}
	offset := intFromWord(data[:32])
	if offset < 0 || offset+32 > len(data) {
		return "", fmt.Errorf("invalid ABI string offset %d", offset)
	}
	length := intFromWord(data[offset : offset+32])
	if length == 0 {
		return "", nil
	}
	start := offset + 32
	if length < 0 || start+length > len(data) {
		return "", fmt.Errorf("invalid ABI string length %d", length)
	}
	return strings.TrimSpace(string(data[start : start+length])), nil
}

func decodeABIAddress(raw string) (string, error) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "0x")
	if raw == "" {
		return "", nil
	}
	data, err := hex.DecodeString(raw)
	if err != nil {
		return "", err
	}
	if len(data) < 20 {
		return "", nil
	}
	addr := data[len(data)-20:]
	return "0x" + hex.EncodeToString(addr), nil
}

func isZeroAddress(ethAddress string) bool {
	addr := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ethAddress)), "0x")
	if addr == "" {
		return true
	}
	return strings.Trim(addr, "0") == ""
}

func intFromWord(word []byte) int {
	n := 0
	for _, b := range word {
		if n > (int(^uint(0)>>1)-int(b))/256 {
			return -1
		}
		n = n*256 + int(b)
	}
	return n
}
