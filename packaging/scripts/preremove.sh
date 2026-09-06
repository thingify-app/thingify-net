#!/bin/sh
set -e

if [ "$1" = "remove" ] || [ "$1" = "purge" ]; then
    systemctl stop thingify.service || true
    systemctl disable thingify.service || true
fi

exit 0
