#!/bin/bash
# Pull the encrypted filter-hit report queue out of the VM and decrypt it on
# the host. Prints "<address>.onion<TAB><time>" lines for manual reporting to
# a hotline. Nothing but addresses is ever in the queue.
set -euo pipefail
AGEKEY=$HOME/.config/onion-report/key.txt
out=$(mktemp -d); trap 'rm -rf "$out"' EXIT
ssh onion 'docker run --rm -v onion_report:/r:ro alpine tar -C /r -cf - .' | tar -C "$out" -xf -
shopt -s nullglob
files=("$out"/*.age)
[ ${#files[@]} -eq 0 ] && { echo "no reports"; exit 0; }
for f in "${files[@]}"; do age -d -i "$AGEKEY" "$f"; done | sort -u
