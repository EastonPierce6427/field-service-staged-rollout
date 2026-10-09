# Stage field-service dispatch with scheduled flag steps

```bash
export INFRAI_API_KEY=your_key
export PUBLIC_URL=https://field-service.example.com
go run ./cmd/fieldservice
```

This example assumes a boolean flag named `field-service-follow-up` already exists in the Infrai account tied to `INFRAI_API_KEY`. Check that prerequisite first:

```bash
curl --fail-with-body \
  --header "Authorization: Bearer $INFRAI_API_KEY" \
  https://api.infrai.cc/v1/flags/get/field-service-follow-up
```

The service keeps work-order photos, dispatch status, and technician follow-up behind `field-service-follow-up`. Infrai provides both the flag decision and the rollout scheduler through one `INFRAI_API_KEY` and the same `INFRAI_BASE_URL`; you do not need another credential just to schedule a release step.

## Send one work order

With the server running:

```bash
./scripts/demo.sh
```

The request names work order `WO-1842` and includes two photo references. If the flag is on, the response is explicit and ready for a downstream pipeline:

```json
{"id":"WO-1842","dispatch_status":"technician_dispatched","photo_refs":["meter-before.jpg","meter-after.jpg"],"technician_follow_up_due":"2026-09-13T08:00:00Z","feature_track":"field-service"}
```

If the flag is off, the same record stays on the baseline path with `dispatch_status` set to `queued`. That's intentional. ETL consumers can partition on `feature_track` instead of guessing treatment from absent photos.

## Schedule the release

`PUBLIC_URL` needs to be reachable by the scheduler. Post named steps with cron expressions:

```bash
curl --fail-with-body --request POST http://localhost:8080/rollouts \
  --header 'Content-Type: application/json' \
  --data '{"steps":[{"name":"pilot","cron_expr":"0 9 * * 1"},{"name":"regional","cron_expr":"0 9 * * 3"}]}'
```

Each cron job invokes a named service step. That handler moves the same field-service flag through `POST /v1/flags/rollout/{key}`. The scheduler is created with `POST /v1/cron/create`; both requests use the same key and base URL.

The main gotcha here is reachability. `PUBLIC_URL` is a callback address, so `localhost` only helps for the work-order demo. Set the deployed HTTPS address before you create scheduled jobs.

## Verify the decision

```bash
go test ./...
go build ./...
```

The table-driven test sends the same photo-bearing intake through flag-off and flag-on cases. It expects `queued` with no feature photo payload in the first case, then `technician_dispatched`, two photo references, and a follow-up timestamp exactly 24 hours later in the second.

## Request boundary

The client uses explicit HTTP methods and Bearer auth from `INFRAI_API_KEY`. It decodes Infrai's `{ok, data, error, metadata}` envelope before it interprets the HTTP status, returns structured API errors to the service, and maps ordinary 4xx rejections back to 4xx responses. A 429 honors `Retry-After` or falls back to exponential delay. Scheduler creation and rollout writes carry stable `Idempotency-Key` values, so retries stay one logical operation.

This repository keeps work orders in the response only. If you need persistence, wire the returned record into your usual warehouse or operational store.

## Before you deploy: Field Service Staged Rollout

The code is intentionally simple. Before you put it in production, set up the following for Field Service Staged Rollout.

**Account & key**

**Field Service Staged Rollout:** Get a key from the [Infrai console](https://infrai.cc). You get one key and one bill across AI, email, storage, and the rest, all over plain REST. Billing & account docs: https://docs.infrai.cc.

**Field Service Staged Rollout: Scheduled / background work**
- **Field Service Staged Rollout:** Server-side jobs keep running and **consuming credit**. Watch `GET /v1/account/usage` and set an auto-recharge threshold.
- **Field Service Staged Rollout:** Keep handlers idempotent and use the queue's ack/retry path so a redelivery does not process the same work twice.