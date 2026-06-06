package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"loadcell/tmpl"
)

// sampleCache memoizes parsed templates for SendSample so {{seq}}
// accumulates across successive "Send" clicks of the same request.
// Without this each click would re-Parse and reset the counter to 0.
//
// Cache key is (request id, field), so editing a field re-parses but
// inherits the previous counter — the user sees seq keep climbing
// even after they tweak the URL or body. Different requests (by ID)
// have independent counters; unsaved requests share the empty-ID slot.
type sampleCache struct {
	mu sync.Mutex
	m  map[string]*sampleEntry
}

type sampleEntry struct {
	content string
	tmpl    *tmpl.Template
}

func newSampleCache() *sampleCache {
	return &sampleCache{m: map[string]*sampleEntry{}}
}

func (c *sampleCache) get(id, field, content string) (*tmpl.Template, error) {
	key := id + "\x00" + field
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[key]; ok && e.content == content {
		return e.tmpl, nil
	}
	t, err := tmpl.Parse(content)
	if err != nil {
		return nil, err
	}
	if prev, ok := c.m[key]; ok {
		t.SetSeq(prev.tmpl.SeqValue())
	}
	c.m[key] = &sampleEntry{content: content, tmpl: t}
	return t, nil
}

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
//
// Templated placeholders ({{uuid}}, {{seq}}, {{nowMs}}, {{randInt:a:b}}) in
// the URL, body, and header values are rendered fresh on every Send — same
// rules as the load engine, so the preview matches what workers will fire.
// A bad token returns a non-nil Error so the UI can surface the message
// inline instead of throwing.
func (a *App) SendSample(req SavedRequest) (SampleResponse, error) {
	if strings.TrimSpace(req.URL) == "" {
		return SampleResponse{}, errors.New("url is required")
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}

	urlTmpl, err := a.sample.get(req.ID, "url", req.URL)
	if err != nil {
		return SampleResponse{Error: fmt.Sprintf("url: %v", err)}, nil
	}
	bodyTmpl, err := a.sample.get(req.ID, "body", req.Body)
	if err != nil {
		return SampleResponse{Error: fmt.Sprintf("body: %v", err)}, nil
	}
	renderedBody := bodyTmpl.Render()

	var body io.Reader
	if renderedBody != "" {
		body = strings.NewReader(renderedBody)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, method, urlTmpl.Render(), body)
	if err != nil {
		return SampleResponse{Error: err.Error()}, nil
	}
	for _, h := range req.Headers {
		if h.Key == "" {
			continue
		}
		valTmpl, err := a.sample.get(req.ID, "h:"+h.Key, h.Value)
		if err != nil {
			return SampleResponse{Error: fmt.Sprintf("header %q: %v", h.Key, err)}, nil
		}
		httpReq.Header.Set(h.Key, valTmpl.Render())
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

