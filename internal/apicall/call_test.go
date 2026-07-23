package apicall

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallXMLToJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// serviceKey must arrive verbatim (not double-encoded).
		if got := r.URL.Query().Get("serviceKey"); got != "abc+def==" {
			t.Errorf("serviceKey = %q, want verbatim abc+def==", got)
		}
		if got := r.URL.Query().Get("numOfRows"); got != "10" {
			t.Errorf("numOfRows = %q", got)
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<response><header><resultCode>00</resultCode></header>` +
			`<body><items><item><name>서울</name><count>5</count></item></items></body></response>`))
	}))
	defer srv.Close()

	res, err := Call(context.Background(), srv.URL+"/svc/op", map[string]string{"numOfRows": "10"}, "abc+def==")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	m, ok := res.Body.(map[string]any)
	if !ok {
		t.Fatalf("body not a map: %T", res.Body)
	}
	if _, ok := m["header"]; !ok {
		t.Errorf("converted XML missing header: %v", m)
	}
}

func TestCallServiceKeyHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<OpenAPI_ServiceResponse><cmmMsgHeader>` +
			`<returnReasonCode>30</returnReasonCode>` +
			`<returnAuthMsg>SERVICE_KEY_IS_NOT_REGISTERED_ERROR</returnAuthMsg>` +
			`</cmmMsgHeader></OpenAPI_ServiceResponse>`))
	}))
	defer srv.Close()

	res, err := Call(context.Background(), srv.URL, nil, "wrongkey")
	if res == nil {
		t.Fatal("CallResult must still be returned (surface the body)")
	}
	if err == nil || !strings.Contains(err.Error(), "Encoding") {
		t.Fatalf("expected Encoding/Decoding key hint in error, got %v", err)
	}
}
