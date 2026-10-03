#!/bin/sh
set -eu
/usr/bin/forum-config check
exec /usr/bin/answer run -C /data/conf "$@"
