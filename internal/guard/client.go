package guard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return NewClientWithHTTPClient(baseURL, nil)
}

func NewClientWithHTTPClient(baseURL string, httpClient *http.Client) *Client {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 8 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *Client) CheckURL(ctx context.Context, req URLCheckRequest) (URLCheckResponse, error) {
	var resp URLCheckResponse
	err := c.postJSON(ctx, "/v1/url/check", req, &resp)
	return resp, err
}

func (c *Client) CheckContent(ctx context.Context, req ContentCheckRequest) (ContentCheckResponse, error) {
	var resp ContentCheckResponse
	err := c.postJSON(ctx, "/v1/content/check", req, &resp)
	return resp, err
}

func (c *Client) CheckPackage(ctx context.Context, req PackageCheckRequest) (PackageCheckResponse, error) {
	var resp PackageCheckResponse
	err := c.postJSON(ctx, "/v1/package/check", req, &resp)
	return resp, err
}

func (c *Client) postJSON(ctx context.Context, path string, in any, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("guard request failed: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		return fmt.Errorf("guard returned %s", httpResp.Status)
	}
	if err := json.NewDecoder(httpResp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
