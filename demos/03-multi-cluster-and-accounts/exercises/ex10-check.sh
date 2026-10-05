#!/usr/bin/env bash
# Exercise 10's checks. A wrapper only -- every check lives in lab/10-hub-meta-leader.sh.
#   ./exercises/ex10-check.sh          every step
#   ./exercises/ex10-check.sh step1    one step
exec bash "$(dirname "$0")/../lab/10-hub-meta-leader.sh" "$@"
