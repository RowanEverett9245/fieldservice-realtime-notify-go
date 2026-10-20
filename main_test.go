package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShouldNotify(t *testing.T) {
	tests := []struct {
		name  string
		event WorkOrderEvent
		want  bool
	}{
		{"dispatch", WorkOrderEvent{Status: "dispatched"}, true},
		{"photo", WorkOrderEvent{PhotoCount: 1}, true},
		{"follow up", WorkOrderEvent{FollowUp: "call customer"}, true},
		{"internal edit", WorkOrderEvent{Status: "assigned"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldNotify(tt.event); got != tt.want {
				t.Fatalf("shouldNotify(%+v) = %v, want %v", tt.event, got, tt.want)
			}
		})
	}
}

func TestNotifyDeletesCreatedChannel(t *testing.T) {
	for _, publishOK := range []bool{true, false} {
		t.Run(map[bool]string{true: "published", false: "publish failed"}[publishOK], func(t *testing.T) {
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				if strings.HasSuffix(r.URL.Path, "/publish") && !publishOK {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"REJECTED","message":"rejected"}}`))
					return
				}
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer server.Close()
			client := &InfraiClient{BaseURL: server.URL, Key: "test", HTTP: server.Client()}
			req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(`{"work_order_id":"test","status":"dispatched"}`))
			w := httptest.NewRecorder()
			notifyHandler(client).ServeHTTP(w, req)
			want := []string{"POST /v1/realtime/channel/create", "POST /v1/realtime/publish", "DELETE /v1/realtime/channel/delete/work-order-test"}
			if strings.Join(calls, ",") != strings.Join(want, ",") {
				t.Fatalf("calls = %v, want %v", calls, want)
			}
			if publishOK && w.Code != http.StatusAccepted || !publishOK && w.Code != http.StatusBadGateway {
				t.Fatalf("status = %d", w.Code)
			}
		})
	}
}
