#!/bin/sh
# Optional isolated test server for a Debian/WSL host without an installed server.
# Extracts packages into a new temp directory; does not install a service or alter system PostgreSQL.
set -eu
taskpg=$(mktemp -d /tmp/cpa-postgres-test.XXXXXX)
cd "$taskpg"
apt-get download postgresql-15 postgresql-client-15
for package in ./*.deb; do dpkg-deb -x "$package" .; done
export PATH="$taskpg/usr/lib/postgresql/15/bin:$PATH"
initdb -D "$taskpg/data" -L "$taskpg/usr/share/postgresql/15" -U cpa_test --auth-local=trust --auth-host=trust --encoding=UTF8 --locale=C
pg_ctl -D "$taskpg/data" -l "$taskpg/server.log" -o "-h 127.0.0.1 -p 55438 -k $taskpg -c timezone=Asia/Shanghai" -w start
createdb -h 127.0.0.1 -p 55438 -U cpa_test cpa_helper_test
printf 'TEST_SERVER_DIR=%s\n' "$taskpg"
printf 'STOP: %s/usr/lib/postgresql/15/bin/pg_ctl -D %s/data -m fast -w stop\n' "$taskpg" "$taskpg"
