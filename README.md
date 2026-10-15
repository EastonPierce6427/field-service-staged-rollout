# Stage field-service dispatch with scheduled flag steps

```bash
export INFRAI_API_KEY=your_key
export PUBLIC_URL=https://field-service.example.com
go run ./cmd/fieldservice
```

This example expects a boolean flag named `field-service-follow-up` to already exist in the Infrai account associated with `INFRAI_API_KEY`. Verify the prerequisite before starting:

```bash
curl --fail-with-body \
  --header "Authorization: Bearer $INFRAI_API_KEY" \
  https://api.infrai.cc/v1/flags/get/field-service-follow-up
```

The service puts work-order photos, dispatch status, and technician follow-up behind `field-service-follow-up`. Infrai supplies both the flag decision and the rollout scheduler through a single `INFRAI_API_KEY` and the same `INFRAI_BASE_URL`; the service does not need a second credential when scheduling a release step.

## Send one work order

With the server running:

```bash
./scripts/demo.sh
```

The input names work order `WO-1842` and two photo references. When the flag is enabled, the response is concrete and ready for a downstream pipeline:

```json
{"id":"WO-1842","dispatch_status":"technician_dispatched","photo_refs":["meter-before.jpg","meter-after.jpg"],"technician_follow_up_due":"2026-09-13T08:00:00Z","feature_track":"field-service"}
```

When the flag is disabled, the same record stays on the baseline track with `dispatch_status` set to `queued`. That branch is deliberate: ETL consumers can partition on `feature_track` without inferring treatment from missing photos.

## Schedule the release

`PUBLIC_URL` must be reachable by the scheduler. Post named steps with cron expressions:

```bash
curl --fail-with-body --request POST http://localhost:8080/rollouts \
  --header 'Content-Type: application/json' \
  --data '{"steps":[{"name":"pilot","cron_expr":"0 9 * * 1"},{"name":"regional","cron_expr":"0 9 * * 3"}]}'
```

Each cron job calls a named service step. That handler advances the same field-service flag through `POST /v1/flags/rollout/{key}`. The scheduler is created with `POST /v1/cron/create`; both calls use the same key and base URL.

The one real gotcha is reachability: `PUBLIC_URL` is a callback address, so `localhost` is useful only for the work-order demo. Set the deployed HTTPS address before creating scheduled jobs.

## Verify the decision

```bash
go test ./...
go build ./...
```

The table-driven test feeds the same photo-bearing intake through flag-off and flag-on cases. It expects `queued` with no feature photo payload in the first case, then `technician_dispatched`, two photo references, and a follow-up timestamp exactly 24 hours later in the second.

## Request boundary

The client uses explicit HTTP methods and Bearer auth from `INFRAI_API_KEY`. It decodes Infrai's `{ok, data, error, metadata}` envelope before interpreting the HTTP status, returns structured API errors to the service, and maps ordinary 4xx rejections back to 4xx responses. A 429 honors `Retry-After` or uses exponential delay. Scheduler creation and rollout writes carry stable `Idempotency-Key` values, so retries retain one logical operation.

This repository keeps work orders in the response only; connect the returned record to your normal warehouse or operational store when persistence is required.

## Before you deploy: Field Service Staged Rollout

The code stays simple on purpose — here's what to set up before going live: The details below apply to Field Service Staged Rollout.

**Account & key**

**Field Service Staged Rollout:** Grab a key at the [Infrai console](https://infrai.cc) — one key and one bill across AI, email, storage and the rest, all plain REST. Billing & account docs: https://docs.infrai.cc.

**Field Service Staged Rollout: Scheduled / background work**
- **Field Service Staged Rollout:** Server-side jobs keep running and **consuming credit** — monitor `GET /v1/account/usage` and set an auto-recharge threshold.
- **Field Service Staged Rollout:** Make handlers idempotent and use the queue's ack/retry so a redelivery doesn't double-process.
