import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("migration", Path(__file__).with_name("migrate-postgres.py"))
migration = importlib.util.module_from_spec(spec)
spec.loader.exec_module(migration)


def config_text(database="cpa_test"):
    return ("DATABASE=" + database + "\nDB_USER=" + database + "\nDB_PASSWORD=" + "a" * 64
            + "\nAPP_CONTAINER=cpa-test\nIMAGE=example/cpa:0.3.23.114\nREADY_URL=http://127.0.0.1:18317/api/ready\n")


class MigrationTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.path = self.root / "config.env"
        self.path.write_text(config_text())
        self.config = migration.read_config(self.path)

    def runner(self, mode="migrate"):
        result = migration.Runner(self.root, self.config, mode)
        result.report.mkdir(parents=True)
        result.container = {"Id": "original", "HostConfig": {"RestartPolicy": {"Name": "unless-stopped"}}}
        result.log = lambda text: None
        return result

    def test_config_defaults_and_validation(self):
        self.assertEqual(self.config["PG_HOST"], "host.docker.internal")
        self.assertEqual(self.config["PG_PORT"], "63851")
        self.assertEqual(self.config["MAX_CONNS"], "64")
        for suffix in ("\nMAX_CONNS=16\n", "\nPG_HOST=remote.example\n", "\nDB_PASSWORD=duplicate\n"):
            self.path.write_text(config_text() + suffix)
            with self.assertRaises(migration.MigrationError):
                migration.read_config(self.path)
        self.path.write_text(config_text().replace("cpa_test", "bad; DROP ROLE x"))
        with self.assertRaises(migration.MigrationError):
            migration.read_config(self.path)

    def test_url_and_private_file(self):
        url = migration.database_url(self.config, "cpa_test")
        self.assertIn("@host.docker.internal:63851/cpa_test?sslmode=disable", url)
        target = self.root / "secret"
        migration.private_write(target, "first")
        migration.private_write(target, "second")
        self.assertEqual(target.read_text(), "second")
        if os.name != "nt":
            self.assertEqual(target.stat().st_mode & 0o777, 0o600)
            link = self.root / "link"
            link.symlink_to(target)
            with self.assertRaises(migration.MigrationError):
                migration.private_write(link, "changed")
            self.assertEqual(target.read_text(), "second")

    def test_online_snapshot_includes_wal_and_leaves_source(self):
        source = self.root / "source.sqlite3"
        target = self.root / "backup.sqlite3"
        with contextlib.closing(sqlite3.connect(str(source))) as db:
            db.execute("PRAGMA journal_mode=WAL")
            db.execute("CREATE TABLE sample(id INTEGER PRIMARY KEY, amount NUMERIC, note TEXT)")
            db.execute("INSERT INTO sample VALUES (9000000001, 1.12345678, '中文')")
            db.commit()
            before = source.read_bytes()
            migration.snapshot_sqlite(source, target)
            self.assertEqual(source.read_bytes(), before)
            with contextlib.closing(sqlite3.connect(str(target))) as copied:
                self.assertEqual(copied.execute("SELECT * FROM sample").fetchone(), (9000000001, 1.12345678, "中文"))
            with self.assertRaises(migration.MigrationError):
                migration.snapshot_sqlite(source, target)

    def test_password_never_appears_in_error_or_report(self):
        runner = self.runner()
        result = subprocess.CompletedProcess([], 1, "secret=" + self.config["DB_PASSWORD"], "")
        with patch.object(migration.subprocess, "run", return_value=result):
            with self.assertRaises(migration.MigrationError) as raised:
                runner.command(["fake"], report="error.log")
        self.assertNotIn(self.config["DB_PASSWORD"], str(raised.exception))
        self.assertNotIn(self.config["DB_PASSWORD"], (runner.report / "error.log").read_text())

    def test_pre_activation_failure_restores_only_original_container(self):
        runner = self.runner()
        runner.restart_changed = True
        calls = []
        runner.command = lambda args, **kwargs: calls.append(args)
        runner.recover()
        self.assertEqual(calls, [["docker", "update", "--restart=unless-stopped", "original"], ["docker", "start", "original"]])

    def test_post_activation_failure_never_falls_back_to_sqlite(self):
        runner = self.runner()
        runner.restart_changed = True
        runner.activation_attempted = True
        calls = []
        runner.command = lambda args, **kwargs: calls.append(args)
        runner.recover()
        self.assertEqual(calls, [["docker", "stop", "-t", "120", "cpa-test"]])

    def test_rehearsal_order_does_not_activate(self):
        runner = self.runner("rehearse")
        calls = []
        for name in ("preflight", "provision", "backup", "import_and_verify", "activate", "recover"):
            setattr(runner, name, lambda name=name: calls.append(name))
        runner.run()
        self.assertEqual(calls, ["preflight", "provision", "backup", "import_and_verify"])
        self.assertTrue(runner.database.startswith("cpa_test_test_"))

    def test_import_before_verify_before_doctor(self):
        runner = self.runner()
        calls = []
        runner.app_command = lambda args: calls.append(args)
        runner.import_and_verify()
        self.assertEqual([args[0] for args in calls], ["import-sqlite", "verify-sqlite", "doctor"])

    def test_active_environment_refuses_import(self):
        runner = self.runner()
        runner.compose_file.touch()
        runner.active_env.write_text("existing production connection")
        with self.assertRaisesRegex(migration.MigrationError, "already exists"):
            runner.preflight()

    def test_sql_uses_unix_socket_and_not_password_arguments(self):
        runner = self.runner()
        calls = []
        runner.command = lambda args, input_text, **kwargs: calls.append((args, input_text))
        runner.sql("CREATE ROLE test PASSWORD '" + self.config["DB_PASSWORD"] + "';", secret=True)
        args, stdin = calls[0]
        self.assertIn("/var/run/postgresql", args)
        self.assertIn("63851", args)
        self.assertIn("postgres_admin", args)
        self.assertNotIn(self.config["DB_PASSWORD"], " ".join(args))
        self.assertIn("SET log_statement='none'", stdin)


if __name__ == "__main__":
    unittest.main()
