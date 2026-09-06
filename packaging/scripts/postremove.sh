#!/bin/sh
set -e

if [ "$1" = "purge" ]; then
    deluser --system thingify >/dev/null 2>&1 || true
fi

systemctl daemon-reload || true

exit 0
