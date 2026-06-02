package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// SampleResponse is what the request-builder shows for a single "Send" hit
// against the configured target. Body is capped so a huge response can't
// crash the WebView; BodyTruncated lets the UI flag that.
type SampleResponse struct {
	Status        int               `json:"status"`
	StatusText    string            `json:"statusText"`
	Headers       map[string]string `json:"headers"`
	Body          string            `json:"body"`
	BodyBytes     int               `json:"bodyBytes"`
	BodyTruncated bool              `json:"bodyTruncated"`
	ContentType   string            `json:"contentType"`
	ElapsedMs     float64           `json:"elapsedMs"`
	Error         string            `json:"error"`
}

const sampleBodyCap = 1 << 20 // 1 MiB rendered max

// SendSample fires one request defined by req and returns the response. A
// network failure surfaces as a non-nil Error string with Status=0 so the
// UI can render the failure inline rather than throwing.
func (a *App) SendSample(req SavedRequest) (SampleResponse, error) {
	if strings.TrimSpace(req.URL) == "" {
		return SampleResponse{}, errors.New("url is required")
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}

	var body io.Reader
	if req.Body != "" {
		body = strings.NewReader(req.Body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, method, req.URL, body)
	if err != nil {
		return SampleResponse{Error: err.Error()}, nil
	}
	for _, h := range req.Headers {
		if h.Key == "" {
			continue
		}
		httpReq.Header.Set(h.Key, h.Value)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	start := time.Now()
	resp, err := client.Do(httpReq)
	elapsed := time.Since(start)
	if err != nil {
		return SampleResponse{
			ElapsedMs: float64(elapsed.Microseconds()) / 1000,
			Error:     err.Error(),
		}, nil
	}
	defer resp.Body.Close()

	// Read with a hard cap so the WebView doesn't try to render gigabytes.
	limited := io.LimitReader(resp.Body, sampleBodyCap+1)
	raw, _ := io.ReadAll(limited)
	truncated := len(raw) > sampleBodyCap
	if truncated {
		raw = raw[:sampleBodyCap]
	}

	out := SampleResponse{
		Status:        resp.StatusCode,
		StatusText:    resp.Status, // includes the numeric prefix; UI can split
		Headers:       flattenHeaders(resp.Header),
		Body:          string(raw),
		BodyBytes:     len(raw),
		BodyTruncated: truncated,
		ContentType:   resp.Header.Get("Content-Type"),
		ElapsedMs:     float64(elapsed.Microseconds()) / 1000,
	}
	return out, nil
}

func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = strings.Join(v, ", ")
	}
	return out
}
