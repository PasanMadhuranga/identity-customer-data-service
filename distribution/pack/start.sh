#!/usr/bin/env bash
#
# Runs the CDS service in the foreground. Ctrl+C stops it.
#
# A thin entry point over `cds start`, kept at the pack root so that the shape
# matches the other on-premises products. Options are passed through:
#
#   ./start.sh            run CDS
#   ./start.sh --debug    run CDS, logging at DEBUG level
#
# There is no background mode. Running CDS as a service is the job of whatever
# already runs services — a systemd unit, a container runtime, a process
# supervisor — and each of those runs this in the foreground too.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CDS="$HERE/cds"
[ -x "$CDS" ] || { echo "start: $CDS is missing or not executable" >&2; exit 1; }

exec "$CDS" start "$@"
