import datetime as dt
import json
import tempfile
import threading
import unittest
import uuid
from pathlib import Path
from unittest.mock import Mock, patch
from http.server import ThreadingHTTPServer

import requests

from server import CLASSES, Harness, handler_for, validate_input


class HarnessTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="yunshao-push-test-")
        self.config = Path(self.temporary.name) / "config.local.json"
        self.config.write_text(json.dumps({"platform_origin": "https://localhost", "profiles": [{"id": "a", "label": "Test A", "username": "test", "password": "test-only"}]}), encoding="utf-8")
        self.harness = Harness(self.config)

    def tearDown(self):
        self.harness.close()
        self.temporary.cleanup()

    def value(self):
        return {"request_id": str(uuid.uuid4()), "profile_id": "a", "behavior": "reading", "severity": "low", "location": "模拟点位", "occurred_at": dt.datetime.now(dt.timezone.utc).isoformat()}

    def test_unconfigured_account_and_unsupported_types_are_rejected(self):
        for field, value in [("profile_id", "other-account"), ("behavior", "fire"), ("severity", "unknown")]:
            payload = self.value()
            payload[field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                validate_input(payload, self.harness.profiles)

    def test_time_requires_timezone_and_recent_range(self):
        immediate = self.value()
        immediate["occurred_at"] = "now"
        self.assertEqual(validate_input(immediate, self.harness.profiles)["occurred_at"], "now")
        for time_value in ["2026-01-01T12:00", (dt.datetime.now(dt.timezone.utc) - dt.timedelta(days=2)).isoformat(), (dt.datetime.now(dt.timezone.utc) + dt.timedelta(minutes=6)).isoformat()]:
            payload = self.value()
            payload["occurred_at"] = time_value
            with self.subTest(time=time_value), self.assertRaises(ValueError):
                validate_input(payload, self.harness.profiles)

    def test_unknown_fields_and_control_characters_are_rejected(self):
        payload = self.value()
        payload["account_id"] = str(uuid.uuid4())
        with self.assertRaises(ValueError):
            validate_input(payload, self.harness.profiles)

    def test_platform_origin_rejects_query_fragment_and_credentials(self):
        original = json.loads(self.config.read_text(encoding="utf-8"))
        for origin in ["https://localhost?query=1", "https://localhost#fragment", "https://:password@localhost"]:
            with self.subTest(origin=origin), self.assertRaises(ValueError):
                self.config.write_text(json.dumps({**original, "platform_origin": origin}), encoding="utf-8")
                Harness(self.config)
        payload = self.value()
        payload["location"] = "location\nspoof"
        with self.assertRaises(ValueError):
            validate_input(payload, self.harness.profiles)

    def test_idempotency_reuses_job_and_rejects_changed_content(self):
        payload = self.value()
        with patch.object(self.harness, "run_job") as worker:
            first = self.harness.submit(payload)
            second = self.harness.submit(payload)
            self.assertEqual(first["request_id"], second["request_id"])
            # Join the sole worker before checking call count.
            for thread in threading.enumerate():
                if thread.name.endswith("(run_job)"):
                    thread.join(timeout=2)
            self.assertEqual(len(self.harness.jobs), 1)
            payload["location"] = "changed"
            with self.assertRaises(ValueError):
                self.harness.submit(payload)
            self.assertNotIn("fingerprint", first)

    def test_restart_keeps_receipt_and_does_not_resend(self):
        payload = self.value()
        with patch.object(self.harness, "run_job"):
            self.harness.submit(payload)
        restored = Harness(self.config)
        self.assertEqual(restored.jobs[payload["request_id"]]["stage"], "interrupted")
        self.assertEqual(restored.public_job(payload["request_id"])["request_id"], payload["request_id"])
        restored.close()

    def test_observer_uses_read_only_query_and_hides_secrets(self):
        self.harness.config["observer"] = {"container": "test-db", "user": "test-user", "database": "test-db"}
        event_id = str(uuid.uuid4())
        with patch("server.subprocess.run") as run:
            run.return_value.stdout = '{"inbox_received":true,"deliveries":[]}'
            result = self.harness.observe(event_id)
            command = run.call_args.args[0]
            self.assertIn("BEGIN READ ONLY", command[-1])
            self.assertIn(event_id, command[-1])
            self.assertNotIn("endpoint", command[-1])
            self.assertEqual(result["state"], "observed")
        with self.assertRaises(ValueError):
            self.harness.observe("'; DELETE FROM push.deliveries; --")

    def test_transport_requires_host_origin_csrf_and_does_not_leak_password(self):
        http = ThreadingHTTPServer(("127.0.0.1", 0), handler_for(self.harness))
        self.harness.http_origin = f"http://127.0.0.1:{http.server_port}"
        self.harness.authority = f"127.0.0.1:{http.server_port}"
        thread = threading.Thread(target=http.serve_forever, daemon=True)
        thread.start()
        try:
            base = self.harness.http_origin
            response = requests.get(base + "/api/context", timeout=5)
            self.assertEqual(response.status_code, 200)
            self.assertNotIn("password", response.text)
            self.assertEqual(response.headers["Cache-Control"], "no-store")
            self.assertEqual(requests.get(base, headers={"Host": "attacker.example"}, timeout=5).status_code, 403)
            self.assertEqual(requests.post(base + "/api/jobs", json=self.value(), timeout=5).status_code, 403)
            headers = {"Origin": "https://attacker.example", "X-CSRF-Token": self.harness.csrf}
            self.assertEqual(requests.post(base + "/api/jobs", headers=headers, json=self.value(), timeout=5).status_code, 403)
            headers["Origin"] = base
            invalid_token = {**headers, "X-CSRF-Token": "é"}
            self.assertEqual(requests.post(base + "/api/jobs", headers=invalid_token, json=self.value(), timeout=5).status_code, 403)
            with patch.object(self.harness, "run_job"):
                response = requests.post(base + "/api/jobs", headers=headers, json=self.value(), timeout=5)
            self.assertEqual(response.status_code, 202)
            self.assertNotIn("fingerprint", response.text)
        finally:
            http.shutdown()
            http.server_close()
            thread.join(timeout=3)

    def simulator_state(self):
        return {"device_id": str(uuid.uuid4()), "camera_id": str(uuid.uuid4()), "mqtt": {"client_id": "test-client", "username": "test-user", "password": "test-secret"}, "config_version": 1, "parameters": {}}

    def test_failed_initial_mqtt_connection_can_be_retried(self):
        simulator = self.harness.clients["a"]
        simulator.state = self.simulator_state()
        failing, healthy = Mock(), Mock()
        failing.connect.side_effect = OSError("broker unavailable")
        healthy.loop_start.side_effect = simulator.connected.set
        with patch.object(simulator, "login"), patch("server.mqtt.Client", side_effect=[failing, healthy]), patch("server.threading.Thread"):
            with self.assertRaises(OSError):
                simulator.prepare()
            self.assertIsNone(simulator.client)
            self.assertFalse(simulator.connected.is_set())
            failing.disconnect.assert_called_once()
            simulator.prepare()
            self.assertIs(simulator.client, healthy)
            self.assertFalse(simulator.stop.is_set())
            self.assertTrue(simulator.connected.is_set())

    def test_evidence_failures_preserve_created_event_and_do_not_republish(self):
        for failure in ["network", "missing", "oversize"]:
            with self.subTest(failure=failure):
                simulator = self.harness.clients["a"]
                simulator.state = self.simulator_state()
                payload = {**self.value(), "occurred_at": "now"}
                event_id = str(uuid.uuid4())
                simulator.acks[payload["request_id"]] = {"result": "accepted"}
                rules = {c: {"enabled": c == payload["behavior"]} for c in CLASSES}
                rules[payload["behavior"]].update(confidence_threshold=0.8, minimum_duration_ms=1000, cooldown_ms=1000, severity=payload["severity"])
                assignment = {"algorithm_key": "classroom_behavior_detection", "assignment_id": str(uuid.uuid4()), "desired_parameters": {"classroom_rules": rules}, "config_state": "applied"}
                attachment = Path(self.temporary.name) / (failure + ".png")
                if failure != "missing":
                    attachment.write_bytes(b"x" * (4 * 1024 * 1024 + 1 if failure == "oversize" else 16))
                self.harness.config["evidence_file"] = str(attachment)
                receipt, updates = {}, []
                def update(**changes):
                    receipt.update(changes)
                    updates.append(dict(receipt))
                with patch.object(simulator, "prepare"), patch.object(simulator, "heartbeat"), patch.object(simulator, "device", return_value={"algorithms": [assignment]}), patch.object(simulator, "publish") as publish, patch.object(simulator, "request", side_effect=[None, {"items": [{"edge_event_id": payload["request_id"], "event_id": event_id}]}]), patch.object(simulator.api, "put", side_effect=requests.ConnectionError("secret must not leak")):
                    simulator.send(payload, update)
                self.assertEqual(receipt["stage"], "event_created")
                self.assertEqual(receipt["event_id"], event_id)
                self.assertTrue(receipt["detail_url"].endswith(event_id))
                self.assertEqual(receipt["evidence"], "too_large" if failure == "oversize" else "upload_failed")
                self.assertEqual(publish.call_count, 1)
                self.assertNotIn("secret", json.dumps(updates))


if __name__ == "__main__":
    unittest.main()
