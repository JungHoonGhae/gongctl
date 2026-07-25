package apicall

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/JungHoonGhae/gongctl/internal/fetch"
)

// CallResult is a surfaced API response. Body is a map (XML→JSON or JSON),
// or a string when the content isn't structured.
type CallResult struct {
	Status      int    `json:"status"`
	ContentType string `json:"contentType"`
	Body        any    `json:"body"`
}

// Call injects serviceKey, GETs the endpoint, and surfaces the response. The
// key is used verbatim (data.go.kr's Encoding key is already URL-encoded, so
// re-encoding it would break it). It never retries: on a well-known key error
// it returns the body AND an error carrying the Encoding/Decoding hint.
func Call(ctx context.Context, f *fetch.Client, endpoint string, params map[string]string, key string) (*CallResult, error) {
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	full := endpoint
	sep := "?"
	if strings.Contains(endpoint, "?") {
		sep = "&"
	}
	// serviceKey appended raw; other params encoded. A literal "+" must still
	// become %2B — otherwise it's read back as a space by any query parser
	// (Go's included) — but any character already escaped in the key (e.g.
	// %2B, %3D from data.go.kr's Encoding form) is left untouched, so we never
	// double-encode.
	full += sep + "serviceKey=" + strings.ReplaceAll(key, "+", "%2B")
	if enc := q.Encode(); enc != "" {
		full += "&" + enc
	}

	resp, err := f.Get(ctx, full)
	if err != nil {
		// fetch.Get's transport error stringifies with the full request URL
		// (including serviceKey) — never log or persist a serviceKey, so redact
		// it (raw and %2B-escaped forms) before surfacing.
		return nil, fmt.Errorf("call %s: %s", endpoint, redactKey(err.Error(), key))
	}

	res := &CallResult{Status: resp.Status, ContentType: resp.ContentType}
	res.Body = decodeBody(resp.ContentType, resp.Body)

	// A just-approved API answers 403 at the gateway for a while: data.go.kr
	// auto-approves the application instantly but takes minutes to propagate it.
	// Surface that reading so a caller (or agent) doesn't misdiagnose its key.
	if resp.Status == http.StatusForbidden {
		return res, fmt.Errorf("data.go.kr 게이트웨이가 403(Forbidden) — 방금 승인된 API는 " +
			"게이트웨이 반영까지 수 분~1시간 걸릴 수 있습니다. 인증키 자체가 유효한지는 " +
			"이미 승인된 다른 API로 확인해 보세요 (키 문제가 아닐 수 있습니다)")
	}

	// Error surface: never swallow. If the body signals an unregistered key,
	// return the surfaced result plus a hint — the Encoding/Decoding trap.
	if strings.Contains(string(resp.Body), "SERVICE_KEY_IS_NOT_REGISTERED_ERROR") {
		return res, fmt.Errorf("data.go.kr: SERVICE_KEY_IS_NOT_REGISTERED_ERROR — " +
			"인증키 형태(Encoding/Decoding)가 잘못됐을 수 있습니다. " +
			"활용신청 상세의 다른 키(Encoding↔Decoding)로 바꿔 다시 시도하세요")
	}
	return res, nil
}

// redactKey strips a serviceKey from an error string in both its raw and
// %2B-escaped forms (the two forms Call may have put into the request URL).
func redactKey(s, key string) string {
	if key == "" {
		return s
	}
	s = strings.ReplaceAll(s, key, "REDACTED")
	s = strings.ReplaceAll(s, strings.ReplaceAll(key, "+", "%2B"), "REDACTED")
	return s
}

// decodeBody converts the response into a structured value: XML→map, JSON
// passthrough, else the raw string.
func decodeBody(contentType string, raw []byte) any {
	trimmed := strings.TrimSpace(string(raw))
	if strings.Contains(contentType, "json") || strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			return v
		}
	}
	if strings.HasPrefix(trimmed, "<") {
		if m, err := xmlToMap(raw); err == nil {
			return m
		}
	}
	return trimmed
}

// xmlToMap converts XML into a nested map[string]any using the stdlib token
// stream. Repeated sibling elements become a []any. Leaf text becomes a string.
// Attributes and namespaces are dropped — data.go.kr response bodies don't use
// them meaningfully. Good enough to surface; the agent reads the shape.
func xmlToMap(raw []byte) (map[string]any, error) {
	dec := xml.NewDecoder(strings.NewReader(string(raw)))
	root := map[string]any{}
	stack := []map[string]any{root}
	var textBuf strings.Builder

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			textBuf.Reset()
			child := map[string]any{}
			parent := stack[len(stack)-1]
			addChild(parent, t.Name.Local, child)
			stack = append(stack, child)
		case xml.CharData:
			textBuf.Write(t)
		case xml.EndElement:
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			text := strings.TrimSpace(textBuf.String())
			textBuf.Reset()
			if len(cur) == 0 && text != "" {
				// leaf: replace the empty map in the parent with its text
				replaceLast(stack[len(stack)-1], t.Name.Local, text)
			}
		}
	}
	// The document root wraps everything in one element (e.g. <response>);
	// unwrap it so callers see its children directly instead of one extra
	// nesting level that carries no information.
	if len(root) == 1 {
		for _, v := range root {
			if child, ok := v.(map[string]any); ok {
				return child, nil
			}
		}
	}
	return root, nil
}

// addChild inserts value under key, promoting to a slice on repeats.
func addChild(m map[string]any, key string, value any) {
	if existing, ok := m[key]; ok {
		if slice, ok := existing.([]any); ok {
			m[key] = append(slice, value)
		} else {
			m[key] = []any{existing, value}
		}
		return
	}
	m[key] = value
}

// replaceLast swaps the most recently added value under key with v (used to
// turn an empty leaf-map into its text content).
func replaceLast(m map[string]any, key string, v any) {
	if existing, ok := m[key]; ok {
		if slice, ok := existing.([]any); ok {
			slice[len(slice)-1] = v
			m[key] = slice
			return
		}
	}
	m[key] = v
}
