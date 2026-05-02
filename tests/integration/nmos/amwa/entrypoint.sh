#!/bin/sh
# Container entrypoint: bring up dbus + avahi-daemon so dhs's DNS-SD
# layer can delegate to Avahi (sub-millisecond cascade timing per #194),
# then exec dhs with the original CLI args.
#
# We don't use supervisord / s6 — we want the container to die when dhs
# does, and we want dhs's exit code to surface to docker. dbus +
# avahi-daemon run as background processes; if either crashes the
# DBus call from dhs will fail and dhs's stdlib fallback will kick in
# (process keeps going; AMWA conformance degrades to pre-#194 levels).
set -eu

# 1. Start dbus-daemon on the system bus.
mkdir -p /run/dbus
if [ ! -e /run/dbus/system_bus_socket ]; then
    dbus-daemon --system --fork --nopidfile
fi

# 2. Start avahi-daemon with a short startup wait so it can register
#    on the bus before dhs probes for it.
avahi-daemon --no-chroot --daemonize >/dev/null 2>&1 || true

# 3. Wait up to 5s for org.freedesktop.Avahi to appear on the bus.
i=0
while [ $i -lt 50 ]; do
    if dbus-send --system --print-reply --dest=org.freedesktop.Avahi \
        / org.freedesktop.Avahi.Server.GetVersionString \
        >/dev/null 2>&1; then
        break
    fi
    i=$((i + 1))
    sleep 0.1
done

# 4. Exec dhs with the rest of the CLI args. exec replaces this shell
#    so dhs's exit code becomes the container's exit code.
exec /usr/local/bin/dhs "$@"
