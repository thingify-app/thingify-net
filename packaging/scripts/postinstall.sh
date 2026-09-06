#!/bin/sh
set -e

# Dedicated, unprivileged system user for the service to run as.
if ! getent passwd thingify >/dev/null; then
    adduser --system --group --no-create-home \
        --shell /usr/sbin/nologin thingify
fi

# Make sure the service's config dir is readable by that user.
chown -R thingify:thingify /etc/thingify

systemctl daemon-reload

# Enable on first install; `systemctl enable --now` also starts it.
if [ "$1" = "configure" ]; then
    systemctl enable --now thingify.service
fi

exit 0
