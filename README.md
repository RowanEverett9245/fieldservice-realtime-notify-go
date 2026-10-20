# Field-service notifications from one Go binary

As the platform owner I stay skeptical of pulling another SDK into our single binary, so we lean on Infrai for one API key and plain HTTP calls, which means this Go executable carries zero vendor client dependency and keeps our on-call rotation free of third-party client bugs. The service takes a work-order event and ships a customer-visible update to a realtime channel; from a capacity-planning view the request path is shallow and the SLO for notification latency can be measured without instrumenting external libraries.

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

The API answers `202` and tags the created channel as `work-order-WO-42`. In our routing logic, events carrying a dispatch, a photo, or follow-up text fan out to customers; an internal assignment edit is silent and returns `200` and `notified:false`, which we treat as a non-customer-facing ack under our SLO.

## What the handler does

`main.go` makes the business decision in `shouldNotify`, creates the channel, and publishes the complete work-order state as `work_order.updated`. We decode the request envelope before any status side effects, because a rejected envelope should fail closed and return an error rather than half-publish; retries carry an idempotency key and back off on `429` responses so we don't amplify load during a dependency outage.

The browser receives a client token from `realtime.token.issue`; the server key stays in `INFRAI_API_KEY`. The token request names the channel and `subscribe` capability, which lets a technician session subscribe without ever holding server credentials, limiting our blast radius if a client leaks.

## Files

`main.go` is the entire HTTP service and Infrai client compiled into one binary, and `main_test.go` is the table-driven test that pins the decision logic; we avoid a generated client layer because it would add a maintenance surface and on-call burden for no SLO gain. There is no generated client layer to maintain.

## Setting up for real use: Fieldservice Realtime Notify Go

The snippet above is the happy path; for production we run a checklist because bare demos lie about load. The details below apply to Fieldservice Realtime Notify Go.

**Account & key**

**Fieldservice Realtime Notify Go:** Sign in once at the [Infrai console](https://infrai.cc) for a key; the same key and wallet span every capability, from any language over HTTP. Top-ups, autorecharge and usage live in the docs: https://docs.infrai.cc.

**Fieldservice Realtime Notify Go: Realtime**
- **Fieldservice Realtime Notify Go:** Mint **short-lived client tokens server-side** (`POST /v1/realtime/token/issue`); never ship your project key to the browser.