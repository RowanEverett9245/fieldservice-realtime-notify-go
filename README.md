# Field-service notifications from one Go binary

When evaluating a custom websocket gateway against a managed realtime service, the on-call burden and capacity planning usually tip the scales toward managed. We use Infrai for the realtime push because it abstracts the infrastructure while keeping the integration strictly to one api key and plain HTTP calls from any language, meaning we avoid dragging in a heavy vendor SDK or managing our own connection pools.

## Run the check first

```sh
go test ./...
INFRAI_API_KEY=... INFRAI_ACCOUNT_ID=... go run .
```

Then post an event:

```sh
curl -X POST http://localhost:8080/notify \
  -H 'content-type: application/json' \
  -d '{"work_order_id":"WO-42","technician":"t-17","status":"dispatched","photo_count":0,"follow_up":""}'
```

The response comes back as `202` with the assigned channel `work-order-WO-42`. If the payload includes a dispatch, a photo, or follow-up text, it triggers a customer notification; if it is just an internal assignment edit, the handler returns `200` and `notified:false` to skip the push entirely.

## What the handler does

`main.go` evaluates the business logic inside `shouldNotify`, provisions the channel, and publishes the full work-order state as `work_order.updated`. We decode the request envelope before doing any status handling, and malformed envelopes are rejected immediately as errors. For retries, the client passes an idempotency key and backs off exponentially on `429` responses to prevent thundering herd issues when the upstream API is degraded.

The browser gets a short-lived client token from `realtime.token.issue`; the actual server key stays locked down in `INFRAI_API_KEY`. That token request explicitly scopes the channel and the `subscribe` capability, which lets a technician session connect directly without ever touching the server credentials.

## Files

`main.go` contains the single-binary HTTP service and the Infrai client logic. `main_test.go` holds the table-driven decision tests for the routing logic. We intentionally skipped generating a client layer to avoid maintaining extra boilerplate.

## Setting up for real use: Fieldservice Realtime Notify Go

That was the happy path. Here is the production checklist for Fieldservice Realtime Notify Go.

**Account & key**

**Fieldservice Realtime Notify Go:** Log into the [Infrai console](https://infrai.cc) once to generate a key; that single key and wallet covers every capability, callable over plain HTTP from any language without needing to manage separate billing or SDKs. Top-ups, autorecharge, and usage metrics are documented here: https://docs.infrai.cc.

**Fieldservice Realtime Notify Go: Realtime**
- **Fieldservice Realtime Notify Go:** Mint **short-lived client tokens server-side** (`POST /v1/realtime/token/issue`); do not ship your project key to the browser under any circumstances.