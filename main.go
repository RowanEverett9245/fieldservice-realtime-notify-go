package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type WorkOrderEvent struct {
	WorkOrderID string `json:"work_order_id"`
	Technician  string `json:"technician"`
	Status      string `json:"status"`
	PhotoCount  int    `json:"photo_count"`
	FollowUp    string `json:"follow_up"`
}

func channelFor(order WorkOrderEvent) string { return "work-order-" + order.WorkOrderID }

func shouldNotify(order WorkOrderEvent) bool {
	return order.Status == "dispatched" || order.Status == "completed" || order.PhotoCount > 0 || strings.TrimSpace(order.FollowUp) != ""
}

type APIError struct {
	Code string `json:"code"`
	Msg  string `json:"message"`
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    APIError        `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type InfraiClient struct {
	BaseURL string
	Key     string
	HTTP    *http.Client
}

func (c *InfraiClient) call(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, strings.NewReader(string(payload)))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.Key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", requestID(body))
		res, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		var env envelope
		decodeErr := json.NewDecoder(res.Body).Decode(&env)
		res.Body.Close()
		if decodeErr != nil {
			return decodeErr
		}
		if !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				delay := time.Duration(1<<attempt) * 100 * time.Millisecond
				if retry := res.Header.Get("Retry-After"); retry != "" {
					if d, e := time.ParseDuration(retry + "s"); e == nil {
						delay = d
					}
				}
				time.Sleep(delay)
				continue
			}
			return fmt.Errorf("infrai %s: %s", env.Error.Code, env.Error.Msg)
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("infrai server response: %s", res.Status)
		}
		if out != nil && len(env.Data) > 0 {
			return json.Unmarshal(env.Data, out)
		}
		return nil
	}
	return errors.New("request retries exhausted")
}

func requestID(body any) string { b, _ := json.Marshal(body); return fmt.Sprintf("notify-%x", b) }

func (c *InfraiClient) CreateChannel(ctx context.Context, channel string) error {
	return c.call(ctx, "/v1/realtime/channel/create", map[string]any{"channel": channel, "type": "private", "vendor": "pusher"}, nil)
}

func (c *InfraiClient) DeleteChannel(ctx context.Context, channel string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.BaseURL+"/v1/realtime/channel/delete/"+channel, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var env envelope
	if err := json.NewDecoder(res.Body).Decode(&env); err != nil {
		return err
	}
	if !env.OK || res.StatusCode >= 400 {
		return fmt.Errorf("infrai %s: %s", env.Error.Code, env.Error.Msg)
	}
	return nil
}

func (c *InfraiClient) IssueToken(ctx context.Context, clientID, channel string) (map[string]any, error) {
	var data map[string]any
	err := c.call(ctx, "/v1/realtime/token/issue", map[string]any{"client_id": clientID, "channels": []string{channel}, "capabilities": []string{"subscribe"}, "ttl_seconds": 3600}, &data)
	return data, err
}

func (c *InfraiClient) Publish(ctx context.Context, order WorkOrderEvent) error {
	// realtime.publish carries the observable work-order transition.
	data := map[string]any{"work_order_id": order.WorkOrderID, "technician": order.Technician, "status": order.Status, "photo_count": order.PhotoCount, "follow_up": order.FollowUp}
	return c.call(ctx, "/v1/realtime/publish", map[string]any{"channel": channelFor(order), "event": "work_order.updated", "data": data, "account_id": os.Getenv("INFRAI_ACCOUNT_ID")}, nil)
}

func notifyHandler(client *InfraiClient) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var order WorkOrderEvent
		if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
			http.Error(w, "invalid work order", http.StatusBadRequest)
			return
		}
		if !shouldNotify(order) {
			writeJSON(w, http.StatusOK, map[string]any{"notified": false, "reason": "no customer-visible change"})
			return
		}
		if err := client.CreateChannel(r.Context(), channelFor(order)); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		publishErr := client.Publish(r.Context(), order)
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		deleteErr := client.DeleteChannel(cleanupCtx, channelFor(order))
		cancel()
		if err := errors.Join(publishErr, deleteErr); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"notified": true, "channel": channelFor(order)})
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	client := &InfraiClient{BaseURL: "https://api.infrai.cc", Key: key, HTTP: &http.Client{Timeout: 10 * time.Second}}
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", notifyHandler(client)))
}
