package mcpserver

import (
	"context"
	"github.com/JungHoonGhae/gongctl/internal/fetch"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSearchToolRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Write([]byte(`<dl><dt>CSV 테스트 데이터셋 미리보기</dt>` +
			`<dd>설명</dd><a href="/data/12345/fileData.do">x</a></dl>`))
	}))
	defer srv.Close()

	server := New(Deps{
		Fetch:   fetch.New(fetch.WithDelay(0)),
		BaseURL: srv.URL,
	})

	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_datasets",
		Arguments: map[string]any{"keyword": "테스트"},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %+v", res.Content)
	}
}
