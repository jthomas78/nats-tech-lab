#!/bin/sh
# Exercise 04 responder hook. `nats reply --command` runs it once per request.
# It writes what the responder itself received, then prints the reply body.
#   .run/ex04/receipts.log   one line per request: <subject><TAB><body>
# The CLI sets NATS_REQUEST_SUBJECT and NATS_REQUEST_BODY (nats reply --help).
# It uses no PATH and no other variable, in case the CLI passes only those two.
# If the write fails, it exits 1, and the CLI then sends no reply. So a broken
# hook shows up as a failed positive control, not as a silent pass.
dir="${0%/*}/../.run/ex04"
printf '%s\t%s\n' "$NATS_REQUEST_SUBJECT" "$NATS_REQUEST_BODY" >> "$dir/receipts.log" || exit 1
echo 'summary: 3 orders'
