#!/usr/bin/env python3
"""One-file SCM migration runner. Python 3.8+, Linux, root, Docker Compose v2.

Credentials come from <root>/postgres-migration.env, never command arguments.
No production database is dropped, no source SQLite file is upgraded in place.
"""
import argparse
import contextlib
import datetime
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import sqlite3
import subprocess
import sys
import tempfile
import time
import urllib.parse
import urllib.request


class MigrationError(Exception):
    pass


def require(condition, message):
    if not condition:
        raise MigrationError(message)


def read_config(path):
    result = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        key, separator, value = line.partition("=")
        require(separator and re.fullmatch(r"[A-Z_]+", key), "Invalid migration config entry")
        require(key not in result and value and value == value.strip(), "Duplicate/empty migration config entry")
        result[key] = value
    required = ("DATABASE", "DB_USER", "DB_PASSWORD", "APP_CONTAINER", "IMAGE", "READY_URL")
    require(all(k in result for k in required), "Missing required migration config entries")
    for key in ("DATABASE", "DB_USER"):
        require(re.fullmatch(r"[a-z][a-z0-9_]{0,35}", result[key]), "Invalid database/role name")
    require(re.fullmatch(r"[A-Za-z0-9_-]{32,128}", result["DB_PASSWORD"]),
            "DB_PASSWORD must be 32-128 URL-safe random characters")
    result.setdefault("PG_CONTAINER", "postgresql")
    result.setdefault("PG_ADMIN", "postgres_admin")
    result.setdefault("PG_SOCKET", "/var/run/postgresql")
    result.setdefault("PG_PORT", "63851")
    result.setdefault("PG_HOST", "host.docker.internal")
    result.setdefault("MAX_CONNS", "64")
    require(result["PG_HOST"] == "host.docker.internal", "PG_HOST must be host.docker.internal")
    require(result["PG_PORT"].isdigit() and 1 <= int(result["PG_PORT"]) <= 65535, "Invalid PG_PORT")
    require(result["MAX_CONNS"] == "64", "MAX_CONNS must be 64")
    for key in ("PG_CONTAINER", "PG_ADMIN", "APP_CONTAINER"):
        require(re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_.-]*", result[key]), "Invalid container/admin name")
    require(result["PG_SOCKET"].startswith("/") and "\n" not in result["PG_SOCKET"], "Invalid PG_SOCKET")
    require(not result["IMAGE"].endswith(":latest") and not result["IMAGE"].startswith("-"),
            "Use an explicit migration image version, not latest")
    ready = urllib.parse.urlsplit(result["READY_URL"])
    require(ready.scheme == "http" and ready.hostname == "127.0.0.1" and not ready.username
            and ready.path == "/api/ready", "READY_URL must be a local /api/ready endpoint")
    return result


def sql_string(value):
    return "'" + value.replace("'", "''") + "'"


def identifier(value):
    return '"' + value.replace('"', '""') + '"'


def database_url(config, name):
    return "postgres://{}:{}@{}:{}/{}?sslmode=disable".format(
        config["DB_USER"], config["DB_PASSWORD"], config["PG_HOST"], config["PG_PORT"], name)


def private_write(path, content):
    require(not path.is_symlink(), "Refusing to replace a symlink: " + str(path))
    temporary = path.with_name(path.name + ".tmp-" + secrets.token_hex(6))
    try:
        fd = os.open(str(temporary), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(str(temporary), str(path))
        if os.name == "posix":
            directory = os.open(str(path.parent), os.O_RDONLY)
            try:
                os.fsync(directory)
            finally:
                os.close(directory)
    finally:
        if temporary.exists():
            temporary.unlink()


def snapshot_sqlite(source, destination, timeout=1800, log=None):
    require(source.is_file(), "SQLite source is missing")
    require(not destination.exists(), "Snapshot destination already exists")
    require(timeout > 0, "Snapshot timeout must be positive")
    log = log or (lambda message: None)
    started = time.monotonic()
    deadline = started + timeout
    last_log, last_remaining, restarts = started, None, 0

    with contextlib.closing(sqlite3.connect(source.as_uri() + "?mode=ro", uri=True, timeout=30)) as src:
        journal = src.execute("PRAGMA journal_mode").fetchone()[0].lower()
        # A pinned WAL read transaction prevents backup_step from restarting on
        # concurrent commits. Never pin rollback-journal databases while live:
        # their writers would be blocked for the entire snapshot duration.
        if journal == "wal":
            src.execute("BEGIN")
            src.execute("SELECT name FROM sqlite_master LIMIT 1").fetchall()
        try:
            total = src.execute("PRAGMA page_count").fetchone()[0]
            page_size = src.execute("PRAGMA page_size").fetchone()[0]
            log("SQLite snapshot: journal={}, size={:.1f} MiB, timeout={}s, pinned_read={}".format(
                journal, total * page_size / 1048576, timeout, journal == "wal"))

            def progress(status, remaining, total):
                nonlocal last_log, last_remaining, restarts
                now = time.monotonic()
                if status == sqlite3.SQLITE_OK:
                    if last_remaining is not None and remaining >= last_remaining:
                        restarts += 1
                    last_remaining = remaining
                description = "pages={}/{}, elapsed={:.1f}s, progress_resets={}, status={}".format(
                    total - remaining, total, now - started, restarts, status)
                if status == sqlite3.SQLITE_DONE or now - last_log >= 15:
                    log("SQLite snapshot: " + description)
                    last_log = now
                # A completed backup is valid even if its last disk sync crossed
                # the deadline; only abort unfinished work.
                require(status == sqlite3.SQLITE_DONE or now < deadline,
                        "SQLite snapshot timed out (journal={}, {}). "
                        "For rehearsal, use --stop-for-snapshot to briefly stop the app, "
                        "or increase --snapshot-timeout for a large/slow database.".format(journal, description))

            with contextlib.closing(sqlite3.connect(str(destination))) as dst:
                src.backup(dst, pages=1024, progress=progress, sleep=0.05)
        finally:
            if src.in_transaction:
                src.rollback()


class Runner:
    def __init__(self, root, config, mode, snapshot_timeout=1800, stop_for_snapshot=False):
        self.root, self.config, self.mode = root, config, mode
        require(snapshot_timeout > 0, "Snapshot timeout must be positive")
        require(not stop_for_snapshot or mode == "rehearse", "--stop-for-snapshot is only for rehearsal")
        self.snapshot_timeout, self.stop_for_snapshot = snapshot_timeout, stop_for_snapshot
        self.compose_file = root / "docker-compose.yml"
        self.active_env = root / ".database.env"
        self.run_id = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ") + "_" + secrets.token_hex(3)
        self.database = config["DATABASE"]
        if mode == "rehearse":
            self.database += "_test_" + self.run_id.lower()
        require(len(self.database) <= 63, "Generated database name too long")
        self.report = root / "migration-backups" / self.run_id
        self.phase = "preflight"
        self.restart_changed = False
        self.activation_attempted = False
        self.temp_containers = set()
        self.image = None
        self.container = None

    def redact(self, value):
        return value.replace(self.config["DB_PASSWORD"], "[REDACTED]")

    def log(self, message):
        print(self.redact(message), flush=True)

    def command(self, arguments, input_text=None, timeout=1800, report=None):
        # Never echo argv or stdin: docker inspect/config and SQL may contain credentials.
        try:
            result = subprocess.run(arguments, input=input_text, stdout=subprocess.PIPE,
                                    stderr=subprocess.PIPE, text=True, timeout=timeout)
        except subprocess.TimeoutExpired:
            raise MigrationError("Command timed out during " + self.phase) from None
        except OSError:
            raise MigrationError("Cannot execute command during " + self.phase) from None
        output = self.redact(result.stdout + result.stderr)
        if report:
            private_write(self.report / report, output)
        if result.returncode:
            raise MigrationError("Command failed during {} (exit {}): {}".format(
                self.phase, result.returncode, output[-5000:]))
        return result.stdout.strip()

    def inspect(self, name):
        return json.loads(self.command(["docker", "inspect", name], timeout=30))[0]

    def compose(self, *args, override=None):
        command = ["docker", "compose", "--project-directory", str(self.root), "-p", self.project,
                   "-f", str(self.compose_file)]
        if override:
            command += ["-f", str(override)]
        return self.command(command + list(args))

    def sql(self, query, database="postgres", secret=False):
        # The existing container's OS postgres user reaches its local Unix socket.
        # Suppress SQL error-statement logging before sending any password SQL.
        prefix = "SET standard_conforming_strings=on; SET lock_timeout='15s'; SET statement_timeout='120s';\n"
        if secret:
            prefix += "SET log_statement='none'; SET log_min_duration_statement=-1; SET log_min_error_statement='panic';\n"
        return self.command(["docker", "exec", "-i", "--user", "postgres", self.config["PG_CONTAINER"],
                             "psql", "-X", "-qAt", "-w", "-h", self.config["PG_SOCKET"],
                             "-p", self.config["PG_PORT"], "-U", self.config["PG_ADMIN"],
                             "-d", database, "-v", "ON_ERROR_STOP=1"], prefix + query, timeout=150)

    def checkpoint(self, phase):
        self.phase = phase
        private_write(self.report / "state.json", json.dumps({
            "mode": self.mode, "phase": phase, "database": self.database,
            "root": str(self.root), "image": self.image, "source_container": self.container["Id"],
            "source_restart_policy": self.container["HostConfig"]["RestartPolicy"],
            "activation_attempted": self.activation_attempted,
        }, indent=2) + "\n")
        self.log("[{}] {}".format(self.mode, phase))

    def preflight(self):
        require(self.compose_file.is_file(), "Missing docker-compose.yml in --root")
        require(not self.active_env.is_symlink() and (not self.active_env.exists() or
                self.active_env.is_file() and self.active_env.stat().st_size == 0),
                ".database.env already exists with content: do not repeat import or switch back to SQLite")
        self.container = self.inspect(self.config["APP_CONTAINER"])
        require(self.container["State"]["Running"], "Source app must be running before migration")
        require(not any(e.startswith("CPA_HELPER_DATABASE_URL=") and e.split("=", 1)[1]
                        for e in self.container["Config"]["Env"]), "Source app already uses PostgreSQL")
        labels = self.container["Config"].get("Labels", {})
        require(labels.get("com.docker.compose.service") == "cpa-helper", "Wrong Compose service/container")
        self.project = labels.get("com.docker.compose.project", "")
        require(re.fullmatch(r"[a-z0-9][a-z0-9_-]*", self.project), "Missing Compose project identity")
        mounts = [m for m in self.container["Mounts"] if m["Destination"] == "/app/data"]
        require(len(mounts) == 1 and mounts[0]["Type"] == "bind", "Expected one bind mount at /app/data")
        self.data = Path(mounts[0]["Source"]).resolve()
        require(self.data == (self.root / "data/helper").resolve(), "Data bind mount does not match --root/data/helper")
        require((self.data / "db/cpa_helper.sqlite3").is_file(), "Missing source SQLite database")
        rendered = json.loads(self.compose("config", "--format", "json"))
        service = rendered["services"]["cpa-helper"]
        require(service.get("container_name") == self.config["APP_CONTAINER"], "Compose container mismatch")
        require(not service.get("environment", {}).get("CPA_HELPER_DATABASE_URL"), "Compose already selects PostgreSQL")
        self.verify_environment_file()
        hosts = service.get("extra_hosts", {})
        if isinstance(hosts, list):
            hosts = dict(item.replace("=", ":", 1).split(":", 1) for item in hosts)
        require(hosts.get("host.docker.internal") == "host-gateway", "Missing host.docker.internal:host-gateway")
        require(service.get("network_mode") != "host", "This runner expects the existing bridge deployment")
        self.network = next(iter(self.container["NetworkSettings"]["Networks"]), "")
        require(self.network, "Source container has no Docker network")
        require(self.inspect(self.config["PG_CONTAINER"])["State"]["Running"], "PostgreSQL container is not running")
        require(int(self.sql("SHOW server_version_num;")) >= 150000, "PostgreSQL 15+ required")
        capacity = int(self.sql("SHOW max_connections;"))
        require(capacity >= 74, "PostgreSQL max_connections must leave room for the 64-connection pool and operations")
        self.command(["docker", "pull", self.config["IMAGE"]], timeout=900)
        self.image = json.loads(self.command(["docker", "image", "inspect", self.config["IMAGE"]]))[0]["Id"]
        help_text = self.command(["docker", "run", "--rm", "--network", "none", "--entrypoint", "/app/cpa-helper", self.image, "help"])
        require("import-sqlite" in help_text and "verify-sqlite" in help_text, "Image has no SQLite import support")
        self.report.parent.mkdir(mode=0o700, exist_ok=True)
        self.report.mkdir(mode=0o700)
        self.checkpoint("preflight-passed")

    def verify_environment_file(self):
        # Older Compose resolves away env_file even with --no-interpolate. Ask
        # Compose to resolve a private synthetic project, without touching the
        # installed connection file or ever starting a container.
        marker = "scm-migration-probe-" + secrets.token_hex(16)
        with tempfile.TemporaryDirectory(prefix="cpa-compose-probe-") as directory:
            probe = Path(directory)
            shutil.copyfile(str(self.compose_file), str(probe / "docker-compose.yml"))
            private_write(probe / ".database.env", "CPA_HELPER_DATABASE_URL=" + marker
                          + "\nCPA_HELPER_DB_MAX_CONNS=64\n")
            rendered = json.loads(self.command(["docker", "compose", "--project-directory", str(probe),
                "-p", self.project, "-f", str(probe / "docker-compose.yml"), "config", "--format", "json"]))
            environment = rendered["services"]["cpa-helper"].get("environment", {})
            require(environment.get("CPA_HELPER_DATABASE_URL") == marker
                    and environment.get("CPA_HELPER_DB_MAX_CONNS") == "64",
                    "Publish Compose with env_file: ./.database.env; database settings must not be overridden")

    def provision(self):
        self.checkpoint("provision-database")
        role, role_literal = identifier(self.config["DB_USER"]), sql_string(self.config["DB_USER"])
        marker = "cpa-helper-migration:" + self.config["DB_USER"]
        existing = self.sql("SELECT COALESCE(shobj_description(oid,'pg_authid'),'') || '|' || "
                            "(rolcanlogin AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole "
                            "AND NOT rolreplication AND NOT rolbypassrls)::text "
                            "FROM pg_roles WHERE rolname=" + role_literal + ";")
        if existing:
            require(existing == marker + "|true", "Existing role is not this migration's restricted application role")
            require(self.sql("SELECT COUNT(*) FROM pg_auth_members WHERE member=(SELECT oid FROM pg_roles WHERE rolname="
                             + role_literal + ");") == "0", "Application role unexpectedly has role memberships")
        else:
            self.sql("BEGIN; CREATE ROLE " + role + " LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD "
                     + sql_string(self.config["DB_PASSWORD"]) + "; COMMENT ON ROLE " + role + " IS " + sql_string(marker) + "; COMMIT;", secret=True)
        exists = self.sql("SELECT COUNT(*) FROM pg_database WHERE datname=" + sql_string(self.database) + ";")
        if exists == "0":
            self.sql("CREATE DATABASE " + identifier(self.database) + " OWNER " + role
                     + " TEMPLATE template0 ENCODING 'UTF8' LOCALE_PROVIDER libc LC_COLLATE 'C' LC_CTYPE 'C';")
            self.sql("REVOKE ALL ON DATABASE " + identifier(self.database) + " FROM PUBLIC;"
                     " COMMENT ON DATABASE " + identifier(self.database) + " IS " + sql_string(marker + ":" + self.run_id) + ";")
        else:
            owner = self.sql("SELECT pg_get_userbyid(datdba) || '|' || COALESCE(shobj_description(oid,'pg_database'),'') "
                             "FROM pg_database WHERE datname=" + sql_string(self.database) + ";")
            require(owner.startswith(self.config["DB_USER"] + "|" + marker + ":"),
                    "Target database already exists and is not owned by this migration")
            objects = self.sql("SELECT COUNT(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace "
                               "WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname <> 'information_schema';", database=self.database)
            require(objects == "0", "Target database is not empty; refusing to overwrite it")
            self.log("Reusing this migration's empty database after an earlier pre-activation failure.")
        self.sql("REVOKE CREATE ON SCHEMA public FROM PUBLIC; GRANT USAGE,CREATE ON SCHEMA public TO " + role + ";", database=self.database)
        private_write(self.report / "database.env", "CPA_HELPER_DATABASE_URL=" + database_url(self.config, self.database)
                      + "\nCPA_HELPER_DB_MAX_CONNS=64\n")
        # Test the exact application network/host mapping BEFORE stopping the old app.
        pg_image = self.inspect(self.config["PG_CONTAINER"])["Image"]
        private_write(self.report / "pgpass", "{}:{}:{}:{}:{}\n".format(
            self.config["PG_HOST"], self.config["PG_PORT"], self.database, self.config["DB_USER"], self.config["DB_PASSWORD"]))
        self.command(["docker", "run", "--rm", "--network", self.network, "--add-host", "host.docker.internal:host-gateway",
                      "--mount", "type=bind,src={},dst=/run/pgpass,readonly".format(self.report / "pgpass"),
                      "-e", "PGPASSFILE=/run/pgpass", "-e", "PGCONNECT_TIMEOUT=10", "--entrypoint", "psql", pg_image,
                      "-X", "-w", "-h", self.config["PG_HOST"], "-p", self.config["PG_PORT"], "-U", self.config["DB_USER"],
                      "-d", self.database, "-v", "ON_ERROR_STOP=1", "-c", "SELECT 1;"], timeout=60)

    def app_command(self, command, sqlite=False):
        name = "cpa-migrate-" + secrets.token_hex(8)
        self.temp_containers.add(name)
        arguments = ["docker", "run", "--rm", "--name", name, "--network", "none" if sqlite else self.network,
                     "--add-host", "host.docker.internal:host-gateway", "-e", "TZ=Asia/Shanghai",
                     "--mount", "type=bind,src={},dst=/app/data{}".format(self.report / "sqlite-current", "" if sqlite else ",readonly")]
        if sqlite:
            arguments += ["-e", "CPA_HELPER_DATABASE_URL="]
        else:
            arguments += ["--env-file", str(self.report / "database.env")]
        arguments += ["--entrypoint", "/app/cpa-helper", self.image] + command
        try:
            output = self.command(arguments, timeout=7200, report=command[0] + ".log")
            self.log(output)
        finally:
            # Killing a Docker CLI alone can leave its server-side container running.
            subprocess.run(["docker", "rm", "-f", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30)
            self.temp_containers.discard(name)

    def stop_source(self):
        self.checkpoint("stopping-sqlite")
        self.restart_changed = True
        self.command(["docker", "update", "--restart=no", self.container["Id"]])
        self.command(["docker", "stop", "-t", "120", self.container["Id"]], timeout=150)
        stopped = self.inspect(self.container["Id"])
        require(not stopped["State"]["Running"], "Source container did not stop")
        require(stopped["State"].get("ExitCode") != 137 and not stopped["State"].get("OOMKilled"),
                "Source needed forced termination; verify in-flight usage before retrying")

    def restart_source(self):
        policy = self.container["HostConfig"]["RestartPolicy"]
        restart = policy["Name"] or "no"
        if restart == "on-failure" and policy.get("MaximumRetryCount"):
            restart += ":" + str(policy["MaximumRetryCount"])
        self.command(["docker", "update", "--restart=" + restart, self.container["Id"]])
        self.command(["docker", "start", self.container["Id"]])
        self.restart_changed = False
        self.log("The original SQLite container and restart policy have been restored.")

    def backup(self):
        if self.mode == "migrate" or self.stop_for_snapshot:
            self.stop_source()
        if self.mode == "migrate":
            self.checkpoint("backup-original")
            shutil.copytree(str(self.data), str(self.report / "data-original"), symlinks=True)
            shutil.copy2(str(self.compose_file), str(self.report / "compose-original.yml"))
            if (self.root / ".env").is_file():
                shutil.copy2(str(self.root / ".env"), str(self.report / "compose-original.env"))
        self.checkpoint("snapshot-sqlite")
        target = self.report / "sqlite-current/db"
        target.mkdir(parents=True, mode=0o700)
        try:
            snapshot_sqlite(self.data / "db/cpa_helper.sqlite3", target / "cpa_helper.sqlite3",
                            timeout=self.snapshot_timeout, log=self.log)
        finally:
            if self.stop_for_snapshot:
                # Restore the unchanged source before upgrading/importing the
                # disposable snapshot, including on timeout or interruption.
                self.restart_source()
        if self.stop_for_snapshot:
            self.checkpoint("sqlite-restored")
        # Upgrade only the disposable snapshot; never the source or recovery copy.
        self.app_command(["migrate"], sqlite=True)

    def import_and_verify(self):
        for command in ("import-sqlite", "verify-sqlite", "doctor"):
            self.checkpoint(command)
            arguments = [command]
            if command != "doctor":
                arguments += ["--source", "/app/data/db/cpa_helper.sqlite3"]
            self.app_command(arguments)

    def activate(self):
        # Fail closed from this point: a started PG app can immediately write via
        # background tasks. Never automatically restart old SQLite afterwards.
        self.activation_attempted = True
        self.checkpoint("activating-postgresql")
        private_write(self.active_env, (self.report / "database.env").read_text())
        override = self.report / "image.compose.json"
        private_write(override, json.dumps({"services": {"cpa-helper": {"image": self.image, "pull_policy": "never"}}}))
        rendered = json.loads(self.compose("config", "--format", "json", override=override))
        environment = rendered["services"]["cpa-helper"].get("environment", {})
        require(environment.get("CPA_HELPER_DATABASE_URL") == database_url(self.config, self.database)
                and environment.get("CPA_HELPER_DB_MAX_CONNS") == "64",
                "Compose did not load the verified PostgreSQL settings; application was not started")
        self.compose("up", "-d", "--no-deps", "--pull", "never", "--force-recreate", "cpa-helper", override=override)
        running = self.inspect(self.config["APP_CONTAINER"])
        env = dict(item.split("=", 1) for item in running["Config"]["Env"] if "=" in item)
        require(env.get("CPA_HELPER_DATABASE_URL") == database_url(self.config, self.database)
                and env.get("CPA_HELPER_DB_MAX_CONNS") == "64", "Started container has wrong database settings")
        self.command(["docker", "exec", self.config["APP_CONTAINER"], "/app/cpa-helper", "doctor"], report="active-doctor.log")
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            try:
                with opener.open(self.config["READY_URL"], timeout=3) as response:
                    if response.status == 200:
                        self.checkpoint("completed")
                        self.log("PostgreSQL migration complete. Keep .database.env on subsequent SCM deployments.")
                        return
            except OSError:
                pass
            time.sleep(2)
        raise MigrationError("PostgreSQL app readiness did not pass; do not fall back to stale SQLite")

    def recover(self):
        if self.activation_attempted:
            # Ensure an uncertain/partially started PG app does not keep running.
            self.log("Activation was attempted: SQLite will NOT be restarted. Preserve PostgreSQL and inspect the report.")
            try:
                self.command(["docker", "stop", "-t", "120", self.config["APP_CONTAINER"]], timeout=150)
            except MigrationError:
                self.log("Could not stop the application; check its state manually.")
        elif self.restart_changed:
            self.restart_source()

    def run(self):
        try:
            self.preflight()
            self.provision()
            self.backup()
            self.import_and_verify()
            if self.mode == "migrate":
                self.activate()
            else:
                self.checkpoint("rehearsal-passed")
                self.log("Rehearsal passed. " + (
                    "The original SQLite app was restored immediately after the snapshot. " if self.stop_for_snapshot else
                    "Production was not stopped. ") + "No database switch or second collector was started.")
        except BaseException:
            self.recover()
            raise
        finally:
            if self.report.exists():
                self.log("Report/recovery directory: " + str(self.report))
                if self.mode == "rehearse":
                    self.log("Isolated rehearsal database retained for inspection: " + self.database)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("rehearse", "migrate"))
    parser.add_argument("--root", required=True, type=Path, help="Installed CPA-Helper tenant directory")
    parser.add_argument("--traffic-paused", action="store_true", help="Confirm ingress stopped and usage queue/in-flight work drained")
    parser.add_argument("--stop-for-snapshot", action="store_true",
                        help="Rehearsal only: stop the app during snapshot, then immediately restore the original container")
    parser.add_argument("--snapshot-timeout", type=int, default=1800, metavar="SECONDS",
                        help="SQLite snapshot time limit in seconds (default: 1800)")
    args = parser.parse_args()
    try:
        require(sys.platform == "linux" and os.geteuid() == 0, "Run as root on the Linux Docker host")
        require(args.root.is_absolute(), "--root must be an absolute path")
        root = args.root.resolve(strict=True)
        require(root not in (Path("/"), Path("/usr"), Path("/usr/local"), Path("/root")), "Invalid application root")
        require(args.mode != "migrate" or args.traffic_paused, "Production requires --traffic-paused after stopping ingress and draining usage")
        config_path = root / "postgres-migration.env"
        require(config_path.is_file() and not config_path.is_symlink(), "Missing regular postgres-migration.env")
        os.chmod(str(config_path), 0o600)
        config = read_config(config_path)
        os.umask(0o077)
        import fcntl
        with open("/run/lock/cpa-helper-postgres-migration.lock", "a") as lock:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                raise MigrationError("Another CPA-Helper migration is running on this host") from None

            def interrupted(_signum, _frame):
                raise MigrationError("Migration interrupted by signal")

            signal.signal(signal.SIGTERM, interrupted)
            Runner(root, config, args.mode, snapshot_timeout=args.snapshot_timeout,
                   stop_for_snapshot=args.stop_for_snapshot).run()
        return 0
    except (MigrationError, OSError, ValueError, KeyError) as error:
        # Parser errors are deliberately generic; never print configuration contents.
        message = str(error) if isinstance(error, MigrationError) else "Invalid configuration or filesystem operation failed"
        if "config" in locals():
            message = message.replace(config["DB_PASSWORD"], "[REDACTED]")
        print("ERROR: " + message, file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
