"""Opt-in real Docker acceptance. Creates only isolated, uniquely named fixtures.

CPA_MIGRATION_DOCKER_TEST=1 python3 scripts/test_migrate_postgres_docker.py
Requires local root Docker access and the two images below. Never reads conf passwords.
"""
import contextlib
import datetime
import importlib.util
import json
import os
from pathlib import Path
import secrets
import shutil
import sqlite3
import subprocess
import tempfile
import time
import unittest
import urllib.request

spec = importlib.util.spec_from_file_location("migration", Path(__file__).with_name("migrate-postgres.py"))
migration = importlib.util.module_from_spec(spec)
spec.loader.exec_module(migration)

APP_IMAGE = "registry.hanada.info/hanada/cpa-helper:0.3.23.114"
PG_IMAGE = "dockerhub.hanada.info/library/postgres:17-bookworm"


@unittest.skipUnless(os.environ.get("CPA_MIGRATION_DOCKER_TEST") == "1", "opt-in isolated Docker acceptance")
class DockerAcceptance(unittest.TestCase):
    def cmd(self, *args):
        result = subprocess.run(list(args), text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=900)
        if result.returncode:
            self.fail("Fixture command failed: " + result.stderr[-3000:])
        return result.stdout.strip()

    def test_rehearsal_failure_recovery_production_and_redeploy(self):
        require = migration.require
        require(os.geteuid() == 0, "test needs root")
        root = Path(tempfile.mkdtemp(prefix="cpa-migration-acceptance-"))
        token = secrets.token_hex(5)
        project, pg = "cpa-migration-" + token, "cpa-migration-pg-" + token
        container = project + "-app"
        # Dynamic published port avoids touching the shared/production port.
        migration.private_write(root / "pg.env", "POSTGRES_USER=postgres_admin\nPOSTGRES_PASSWORD=" + secrets.token_hex(32) + "\n")
        self.cmd("docker", "run", "-d", "--name", pg, "--env-file", str(root / "pg.env"), "-p", "0:5432", PG_IMAGE)
        compose = root / "docker-compose.yml"
        port = json.loads(self.cmd("docker", "inspect", pg))[0]["NetworkSettings"]["Ports"]["5432/tcp"][0]["HostPort"]
        try:
            for _ in range(60):
                probe = subprocess.run(["docker", "exec", pg, "pg_isready", "-U", "postgres_admin"], capture_output=True)
                if probe.returncode == 0:
                    break
                time.sleep(1)
            else:
                self.fail("test PostgreSQL did not become ready")
            # Port differs inside PG, so forward the Unix socket's expected port
            # via a socket filename alias, without altering the server or script.
            self.cmd("docker", "exec", pg, "ln", "-s", ".s.PGSQL.5432", "/var/run/postgresql/.s.PGSQL." + port)
            data = root / "data/helper"
            data.mkdir(parents=True)
            spec = {"name": project, "services": {"cpa-helper": {
                "image": APP_IMAGE, "container_name": container, "restart": "unless-stopped",
                "extra_hosts": ["host.docker.internal:host-gateway"],
                "env_file": ["./.database.env"],
                "ports": ["127.0.0.1::18317"], "volumes": ["./data/helper:/app/data"]}}}
            compose.write_text(json.dumps(spec))
            migration.private_write(root / ".database.env", "")
            self.cmd("docker", "compose", "-f", str(compose), "up", "-d", "--pull", "never")
            original = json.loads(self.cmd("docker", "inspect", container))[0]
            app_port = original["NetworkSettings"]["Ports"]["18317/tcp"][0]["HostPort"]
            ready_url = "http://127.0.0.1:" + app_port + "/api/ready"
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            for _ in range(60):
                try:
                    with opener.open(ready_url, timeout=2) as response:
                        if response.status == 200:
                            break
                except OSError:
                    time.sleep(1)
            else:
                self.fail("fixture application did not become ready")
            # Fix the same host port for the recreation performed by production mode.
            spec["services"]["cpa-helper"]["ports"] = ["127.0.0.1:" + app_port + ":18317"]
            compose.write_text(json.dumps(spec))
            with contextlib.closing(sqlite3.connect(str(data / "db/cpa_helper.sqlite3"))) as db:
                stamp = datetime.datetime.now(datetime.timezone(datetime.timedelta(hours=8))).isoformat(timespec="microseconds")
                user = db.execute("INSERT INTO users(username,password_hash,nickname,created_at,updated_at) VALUES ('migration-fixture','test-hash','迁移测试',?,?)", (stamp, stamp)).lastrowid
                record = db.execute("INSERT INTO usage_records(created_at,timestamp,usage_username,model,input_tokens,total_tokens,cost_usd,dedupe_key,raw_json) "
                                    "VALUES (?,?,'migration-fixture','fixture-model',1234,1234,0.12345678,'fixture-dedupe','{}')", (stamp, stamp)).lastrowid
                db.execute("INSERT INTO usage_ingest_dedup VALUES ('fixture-dedupe',?,?,?)", (stamp, stamp, record))
                db.execute("INSERT INTO quota_cards(user_id,kind,name,amount_usd,used_usd,created_at,updated_at) "
                           "VALUES (?,'credit','fixture-card',20,0.12345678,?,?)", (user, stamp, stamp))
                db.commit()
            config = {"DATABASE": "cpa_accept_" + token, "DB_USER": "cpa_accept_" + token,
                      "DB_PASSWORD": secrets.token_hex(32), "APP_CONTAINER": container,
                      "IMAGE": APP_IMAGE, "READY_URL": ready_url, "PG_CONTAINER": pg,
                      "PG_ADMIN": "postgres_admin", "PG_SOCKET": "/var/run/postgresql",
                      "PG_HOST": "host.docker.internal", "PG_PORT": port, "MAX_CONNS": "64"}
            runner = migration.Runner(root, config, "rehearse")
            runner.run()
            self.assertEqual((root / ".database.env").read_bytes(), b"")
            self.assertEqual(json.loads(self.cmd("docker", "inspect", container))[0]["Id"], original["Id"])
            self.assertEqual(json.loads((runner.report / "state.json").read_text())["phase"], "rehearsal-passed")

            wrong = migration.Runner(root, dict(config, DB_PASSWORD=secrets.token_hex(32)), "rehearse")
            with self.assertRaisesRegex(migration.MigrationError, "authentication failed"):
                wrong.run()
            unchanged = json.loads(self.cmd("docker", "inspect", container))[0]
            self.assertEqual(unchanged["Id"], original["Id"])
            self.assertTrue(unchanged["State"]["Running"])
            self.assertEqual((root / ".database.env").read_bytes(), b"")

            # Import-stage failure must recover the same untouched SQLite container.
            failing_config = dict(config, DATABASE="cpa_failure_" + token, DB_USER="cpa_failure_" + token)
            failing = migration.Runner(root, failing_config, "migrate")
            def fail_import():
                raise migration.MigrationError("synthetic import-stage failure")
            failing.import_and_verify = fail_import
            with self.assertRaisesRegex(migration.MigrationError, "synthetic"):
                failing.run()
            restored = json.loads(self.cmd("docker", "inspect", container))[0]
            self.assertEqual(restored["Id"], original["Id"])
            self.assertTrue(restored["State"]["Running"])
            self.assertEqual(restored["HostConfig"]["RestartPolicy"]["Name"], "unless-stopped")
            self.assertEqual((root / ".database.env").read_bytes(), b"")

            # Retry the empty, script-owned target from the failed import attempt.
            config = failing_config
            runner = migration.Runner(root, config, "migrate")
            runner.run()
            self.assertEqual(json.loads((runner.report / "state.json").read_text())["phase"], "completed")
            self.assertTrue((runner.report / "data-original/db/cpa_helper.sqlite3").exists())
            self.assertIn("CPA_HELPER_DB_MAX_CONNS=64", (root / ".database.env").read_text())
            self.assertEqual(runner.sql("SELECT COUNT(*) FROM users WHERE nickname='迁移测试';", database=config["DATABASE"]), "1")
            self.assertEqual(runner.sql("SELECT cost_usd FROM usage_records WHERE dedupe_key='fixture-dedupe';", database=config["DATABASE"]), "0.12345678")
            self.assertEqual(runner.sql("SELECT amount_usd-used_usd FROM quota_cards WHERE name='fixture-card';", database=config["DATABASE"]), "19.87654322")
            for report in runner.report.glob("*.log"):
                self.assertNotIn(config["DB_PASSWORD"], report.read_text())
            # A subsequent ordinary Compose/SCM recreation must retain PostgreSQL.
            self.cmd("docker", "compose", "-f", str(compose), "up", "-d", "--force-recreate", "--pull", "never")
            current = json.loads(self.cmd("docker", "inspect", container))[0]
            self.assertIn("CPA_HELPER_DATABASE_URL=" + migration.database_url(config, config["DATABASE"]), current["Config"]["Env"])
            with self.assertRaisesRegex(migration.MigrationError, "already exists"):
                migration.Runner(root, config, "migrate").run()
        finally:
            subprocess.run(["docker", "compose", "-f", str(compose), "down"], capture_output=True)
            subprocess.run(["docker", "rm", "-f", pg], capture_output=True)
            # Only the exact mkdtemp-owned synthetic fixture directory is removed.
            shutil.rmtree(str(root))


if __name__ == "__main__":
    unittest.main()
