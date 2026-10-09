#!/bin/sh
set -eu

curl --fail-with-body --request POST "http://localhost:8080/work-orders" \
  --header "Content-Type: application/json" \
  --data '{"work_order_id":"WO-1842","photo_refs":["meter-before.jpg","meter-after.jpg"]}'
