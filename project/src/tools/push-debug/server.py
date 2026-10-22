"""Loopback-only test harness; all events use the existing HTTPS + MQTT contracts."""
from __future__ import annotations

import argparse
import atexit
import datetime as dt
import hashlib
import hmac
import json
import logging
import re
import secrets
import subprocess
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

import paho.mqtt.client as mqtt
import requests

CLASSES = ("hand-raising", "reading", "writing")
SEVERITIES = ("low", "medium", "high", "critical")
VERSION = "push-debug-simulator-v1"
LOG = logging.getLogger("push-debug")


def now():
    return dt.datetime.now(dt.timezone.utc).isoformat()


def wait_for(check, timeout=25):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        result = check()
        if result:
            return result
        time.sleep(0.25)
    raise RuntimeError("等待服务确认超时，请查看事件状态后再重试")


def validate_input(value, profiles):
    if not isinstance(value, dict) or set(value) != {"request_id", "profile_id", "behavior", "location", "severity", "occurred_at"}:
        raise ValueError("请求字段不完整或包含未知字段")
    value = dict(value)
    value["request_id"] = str(uuid.UUID(value["request_id"]))
    if value["profile_id"] not in profiles:
        raise ValueError("只能选择已配置的测试账号")
    if value["behavior"] not in CLASSES or value["severity"] not in SEVERITIES:
        raise ValueError("不支持的事件类型或等级")
    location = value["location"]
    if not isinstance(location, str) or not 1 <= len(location.strip()) <= 120 or any(ord(c) < 32 for c in location):
        raise ValueError("地点应为 1–120 个可见字符")
    value["location"] = location.strip()
    if value["occurred_at"] == "now":
        return value
    occurred = dt.datetime.fromisoformat(value["occurred_at"].replace("Z", "+00:00"))
    if occurred.tzinfo is None or not -60 <= (dt.datetime.now(dt.timezone.utc) - occurred).total_seconds() <= 86400:
        raise ValueError("时间需要包含时区，范围为过去 24 小时至未来 1 分钟")
    value["occurred_at"] = occurred.astimezone(dt.timezone.utc).isoformat()
    return value


class Simulator:
    def __init__(self, harness, profile):
        self.harness, self.profile = harness, profile
        self.lock = threading.Lock()
        self.api = requests.Session()
        self.api.verify = harness.config.get("ca_file", True)
        self.api.headers["Origin"] = harness.origin
        self.state_path = harness.private_dir / (profile["id"] + ".device.local.json")
        self.state = json.loads(self.state_path.read_text(encoding="utf-8")) if self.state_path.exists() else None
        self.connected = threading.Event()
        self.acks = {}
        self.client = None
        self.stop = threading.Event()

    def request(self, method, path, expected=(200,), **kwargs):
        response = self.api.request(method, self.harness.origin + path, timeout=20, **kwargs)
        if response.status_code not in expected:
            # Do not leak upstream bodies, secrets, endpoints or transport exception URLs.
            raise RuntimeError(f"平台 {method} {path.split('?')[0]} 返回 HTTP {response.status_code}")
        return response.json() if response.content else None

    def login(self):
        result = self.request("POST", "/api/v1/auth/login", json={"username": self.profile["username"], "password": self.profile["password"]})
        self.api.headers["X-CSRF-Token"] = result["csrf_token"]

    def save(self):
        temporary = self.state_path.with_suffix(".tmp")
        temporary.write_text(json.dumps(self.state, ensure_ascii=False, indent=2), encoding="utf-8")
        temporary.replace(self.state_path)

    def prepare(self):
        self.login()
        if self.state is None:
            device = self.request("POST", "/api/v1/devices", (201,), json={"name": "推送调试模拟设备 · " + self.profile["label"], "ip": "127.0.0.1", "location": "推送联调 · 模拟点位"})
            device_id = device["device_id"]
            camera = self.request("POST", f"/api/v1/devices/{device_id}/cameras", (201,), json={"name": "推送调试模拟摄像头（非真实视频）", "camera_key": "push-debug-camera"})
            ticket = self.request("POST", f"/api/v1/devices/{device_id}/activation-tickets", (201,), json={})
            activation = self.request("POST", "/edge/v1/activate", json={"device_id": device_id, "ticket": ticket["activation_ticket"]})
            self.state = {"device_id": device_id, "camera_id": camera["camera_id"], "mqtt": activation["mqtt"], "config_version": 0, "parameters": {"classroom_rules": {c: {"enabled": False} for c in CLASSES}}}
            self.save()
        if self.client is not None:
            if not self.connected.wait(15):
                raise RuntimeError("MQTT 连接未就绪")
            return
        state = self.state
        self.client = mqtt.Client(mqtt.CallbackAPIVersion.VERSION2, client_id=state["mqtt"]["client_id"], clean_session=False)
        self.client.username_pw_set(state["mqtt"]["username"], state["mqtt"]["password"])
        self.client.tls_set(ca_certs=self.harness.config.get("ca_file"))
        self.client.on_connect = self.on_connect
        self.client.on_disconnect = lambda *args: self.connected.clear()
        self.client.on_message = self.on_message
        self.stop.clear()
        try:
            self.client.connect(self.harness.config.get("mqtt_host", "localhost"), int(self.harness.config.get("mqtt_port", 8883)), 30)
            self.client.loop_start()
            if not self.connected.wait(15):
                raise RuntimeError("MQTT TLS 连接未就绪")
        except Exception:
            self.close()
            self.client = None
            self.connected.clear()
            raise
        threading.Thread(target=self.heartbeats, daemon=True).start()

    def publish(self, suffix, payload):
        return self.client.publish(f"devices/{self.state['device_id']}/{suffix}", json.dumps(payload), qos=1)

    def on_connect(self, client, userdata, flags, reason, properties):
        if reason.is_failure:
            return
        client.subscribe([(f"devices/{self.state['device_id']}/commands", 1), (f"devices/{self.state['device_id']}/event-acks", 1)])
        self.connected.set()

    def on_message(self, client, userdata, message):
        try:
            data = json.loads(message.payload)
            if message.topic.endswith("/event-acks"):
                self.acks[data["event_id"]] = data
            elif message.topic.endswith("/commands"):
                if data["expected_version"] >= self.state["config_version"]:
                    self.state.update(parameters=data["parameters"], config_version=data["expected_version"])
                    self.save()
                self.publish("command-acks", {"schema_version": 1, "device_id": self.state["device_id"], "sent_at": now(), "command_id": data["command_id"], "status": "applied", "reported_config_version": self.state["config_version"], "failure_reason": ""})
        except (ValueError, KeyError, OSError):
            LOG.warning("模拟设备收到无法处理的消息")

    def heartbeat(self):
        self.publish("state", {"schema_version": 1, "device_id": self.state["device_id"], "sent_at": now(), "device_online": True, "npu_state": "not_present", "model_state": "simulated", "stream_state": "not_configured", "mqtt_state": "connected", "desired_config_version": self.state["config_version"], "applied_config_version": self.state["config_version"], "queue_depth": 0, "cameras": [{"camera_key": "push-debug-camera", "camera_state": "simulated", "stream_state": "not_configured"}], "algorithms": [{"algorithm_key": "classroom_behavior_detection", "algorithm_version": VERSION, "runtime_state": "simulated", "capabilities": {"parameters": {}}, "reported_parameters": self.state["parameters"], "reported_config_version": self.state["config_version"]}]})

    def heartbeats(self):
        while not self.stop.is_set():
            if self.connected.is_set():
                self.heartbeat()
            self.stop.wait(15)

    def device(self):
        return self.request("GET", "/api/v1/devices/" + self.state["device_id"])

    def send(self, value, update):
        with self.lock:
            self.prepare()
            self.request("PATCH", "/api/v1/devices/" + self.state["device_id"], json={"location": value["location"]})
            self.heartbeat()
            device = wait_for(lambda: (v if (v := self.device())["algorithms"] else None))
            assignment = next(a for a in device["algorithms"] if a["algorithm_key"] == "classroom_behavior_detection")
            rules = {c: {"enabled": c == value["behavior"]} for c in CLASSES}
            rules[value["behavior"]].update(confidence_threshold=0.8, minimum_duration_ms=1000, cooldown_ms=1000, severity=value["severity"])
            if assignment["desired_parameters"] != {"classroom_rules": rules} or assignment["config_state"] != "applied":
                command = self.request("PUT", f"/api/v1/devices/{self.state['device_id']}/algorithms/{assignment['assignment_id']}/parameters", (202,), json={"expected_version": assignment["desired_version"], "parameters": {"classroom_rules": rules}})
                wait_for(lambda: self.state["config_version"] == command["desired_version"])
                wait_for(lambda: self.device()["algorithms"][0]["config_state"] == "applied")
            self.heartbeat()
            update(stage="rules_applied")
            occurred_at = now() if value["occurred_at"] == "now" else value["occurred_at"]
            event = {"schema_version": 1, "device_id": self.state["device_id"], "event_id": value["request_id"], "camera_id": self.state["camera_id"], "event_type": "classroom.behavior." + value["behavior"], "severity": value["severity"], "occurred_at": occurred_at, "sent_at": now(), "algorithm_key": "classroom_behavior_detection", "algorithm_version": VERSION, "attributes": {"behavior_class": value["behavior"], "confidence": 0.95, "observed_duration_ms": 2000, "rule_config_version": self.state["config_version"]}}
            update(occurred_at=occurred_at)
            self.publish("events", event).wait_for_publish(timeout=10)
            ack = wait_for(lambda: self.acks.get(event["event_id"]))
            update(mqtt_ack=ack["result"], stage="event_acknowledged")
            if ack["result"] not in ("accepted", "duplicate"):
                raise RuntimeError("事件被现有规则拒绝：" + ack.get("reason_code", "unknown"))
            # Event ID exposed to the PWA is owned by event-service, not the edge UUID.
            def lookup():
                events = self.request("GET", "/api/v1/events?limit=100")["items"]
                return next((e for e in events if e.get("edge_event_id") == event["event_id"]), None)
            stored = wait_for(lookup)
            # Persist the authoritative receipt before optional evidence I/O.
            update(stage="event_persisted", event_id=stored["event_id"], detail_url=self.harness.origin + "/events/" + stored["event_id"], evidence="not_uploaded")
            evidence = self.harness.config.get("evidence_file")
            evidence_status = "not_uploaded"
            if evidence:
                try:
                    body = Path(evidence).read_bytes()
                    if len(body) > 4 * 1024 * 1024:
                        evidence_status = "too_large"
                    else:
                        response = self.api.put(self.harness.origin + f"/edge/v1/devices/{self.state['device_id']}/events/{event['event_id']}/evidence", data=body, headers={"Content-Type": "image/png"}, auth=(self.state["mqtt"]["username"], self.state["mqtt"]["password"]), timeout=20)
                        evidence_status = "uploaded" if response.status_code in (200, 201) else "upload_failed"
                except (OSError, requests.RequestException):
                    evidence_status = "upload_failed"
            update(stage="event_created", evidence=evidence_status)

    def close(self):
        self.stop.set()
        if self.client:
            self.client.disconnect()
            self.client.loop_stop()


class Harness:
    def __init__(self, config_path):
        self.config = json.loads(Path(config_path).read_text(encoding="utf-8-sig"))
        self.origin = self.config["platform_origin"].rstrip("/")
        parsed = urlsplit(self.origin)
        if parsed.scheme != "https" or not parsed.hostname or parsed.username is not None or parsed.path or parsed.query or parsed.fragment:
            raise ValueError("platform_origin 必须是 HTTPS origin")
        self.private_dir = Path(config_path).resolve().parent / ".state"
        self.private_dir.mkdir(exist_ok=True)
        self.profiles = {}
        for profile in self.config["profiles"]:
            if not re.fullmatch(r"[a-z0-9_-]{1,40}", profile["id"]) or profile["id"] in self.profiles:
                raise ValueError("profile id 必须唯一且只包含小写字母、数字、下划线、连字符")
            self.profiles[profile["id"]] = profile
        self.clients = {key: Simulator(self, profile) for key, profile in self.profiles.items()}
        self.jobs_path = self.private_dir / "jobs.local.json"
        self.jobs = json.loads(self.jobs_path.read_text(encoding="utf-8")) if self.jobs_path.exists() else {}
        for job in self.jobs.values():
            if job["stage"] not in ("event_created", "failed"):
                job.update(stage="interrupted", error="服务重启，未自动重发；请先核查事件列表")
        self.lock = threading.RLock()
        self.csrf = secrets.token_urlsafe(32)
        self.http_origin = "http://127.0.0.1:" + str(self.config.get("port", 9088))
        self.authority = urlsplit(self.http_origin).netloc
        atexit.register(self.close)

    def persist(self):
        temporary = self.jobs_path.with_suffix(".tmp")
        temporary.write_text(json.dumps(self.jobs, ensure_ascii=False, indent=2), encoding="utf-8")
        temporary.replace(self.jobs_path)

    def submit(self, value):
        value = validate_input(value, self.profiles)
        fingerprint = hashlib.sha256(json.dumps(value, sort_keys=True).encode()).hexdigest()
        key = value["request_id"]
        with self.lock:
            if key in self.jobs:
                if self.jobs[key]["fingerprint"] != fingerprint:
                    raise ValueError("同一个 request_id 不能对应不同表单内容")
                return self.public_job(key)
            self.jobs[key] = {"request_id": key, "profile_id": value["profile_id"], "stage": "queued", "created_at": now(), "fingerprint": fingerprint}
            self.persist()
        threading.Thread(target=self.run_job, args=(key, value), daemon=True).start()
        return self.public_job(key)

    def run_job(self, key, value):
        def update(**changes):
            with self.lock:
                self.jobs[key].update(changes)
                self.persist()
        try:
            self.clients[value["profile_id"]].send(value, update)
        except Exception as exc:
            # Requests exceptions may contain secret-bearing URLs; only controlled errors are exposed.
            message = str(exc) if type(exc) is RuntimeError else "联调失败，请检查平台、测试配置和 MQTT 连接"
            update(stage="failed", error=message)
            LOG.warning("调试任务失败 (%s)", type(exc).__name__)

    def public_job(self, key):
        with self.lock:
            job = {k: v for k, v in self.jobs[key].items() if k != "fingerprint"}
        if job.get("event_id"):
            job["delivery"] = self.observe(job["event_id"])
        return job

    def observe(self, event_id):
        observer = self.config.get("observer")
        if not observer:
            return {"state": "not_configured"}
        event_id = str(uuid.UUID(event_id))
        # UUID is parsed above. Identifiers are fixed server-side configuration and passed without a shell.
        sql = f"""BEGIN READ ONLY;
SELECT json_build_object('inbox_received', EXISTS(SELECT 1 FROM push.inbox WHERE event_id='{event_id}'::uuid AND event_type='event.created'),
'outbox_delivered', EXISTS(SELECT 1 FROM event.outbox WHERE id='{event_id}'::uuid AND delivered_at IS NOT NULL),
'account_active_subscriptions', (SELECT count(*) FROM push.subscriptions s JOIN event.events e ON e.account_id=s.account_id WHERE e.id='{event_id}'::uuid AND s.disabled_at IS NULL),
'deliveries', COALESCE((SELECT json_agg(json_build_object('status',status,'attempts',attempts,'last_class',last_class)) FROM push.deliveries WHERE event_id='{event_id}'::uuid),'[]'::json));
COMMIT;"""
        try:
            result = subprocess.run(["docker", "exec", observer["container"], "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", observer["user"], "-d", observer["database"], "-c", sql], capture_output=True, text=True, timeout=8, check=True, creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
            return {"state": "observed", **json.loads(result.stdout.strip())}
        except (subprocess.SubprocessError, OSError, ValueError, KeyError):
            return {"state": "unavailable"}

    def close(self):
        for client in self.clients.values():
            client.close()


def handler_for(harness):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, format, *args):
            LOG.info("%s %s", self.command, urlsplit(self.path).path)

        def send_body(self, status, body, content_type="application/json; charset=utf-8"):
            encoded = body if isinstance(body, bytes) else json.dumps(body, ensure_ascii=False).encode()
            self.send_response(status)
            for key, value in {"Content-Type": content_type, "Content-Length": str(len(encoded)), "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer", "Content-Security-Policy": "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"}.items():
                self.send_header(key, value)
            self.end_headers()
            self.wfile.write(encoded)

        def host_allowed(self):
            if self.headers.get("Host") != harness.authority:
                self.send_body(403, {"error": "请使用配置的本机地址访问"})
                return False
            return True

        def do_GET(self):
            if not self.host_allowed():
                return
            path = urlsplit(self.path).path
            if path in ("/", "/app.js", "/style.css"):
                name = {"/": "index.html", "/app.js": "app.js", "/style.css": "style.css"}[path]
                content_type = {"/": "text/html; charset=utf-8", "/app.js": "text/javascript; charset=utf-8", "/style.css": "text/css; charset=utf-8"}[path]
                self.send_body(200, Path(__file__).with_name(name).read_bytes(), content_type)
            elif path == "/api/context":
                self.send_body(200, {"csrf_token": harness.csrf, "profiles": [{"id": p["id"], "label": p["label"]} for p in harness.profiles.values()], "platform_origin": harness.origin})
            elif path.startswith("/api/jobs/"):
                key = path.removeprefix("/api/jobs/")
                if key in harness.jobs:
                    self.send_body(200, harness.public_job(key))
                else:
                    self.send_body(404, {"error": "任务不存在"})
            else:
                self.send_body(404, {"error": "地址不存在"})

        def do_POST(self):
            if not self.host_allowed():
                return
            if self.headers.get("Origin") != harness.http_origin or not hmac.compare_digest(self.headers.get("X-CSRF-Token", "").encode(), harness.csrf.encode()):
                self.send_body(403, {"error": "来源或 CSRF 校验失败"})
                return
            if self.path != "/api/jobs":
                self.send_body(404, {"error": "地址不存在"})
                return
            try:
                length = int(self.headers.get("Content-Length", "0"))
                if not 0 < length <= 8192 or self.headers.get("Content-Type") != "application/json":
                    raise ValueError("请求类型或大小不正确")
                value = json.loads(self.rfile.read(length))
                self.send_body(202, harness.submit(value))
            except (ValueError, TypeError, KeyError, AttributeError):
                self.send_body(422, {"error": "表单无效：请检查测试账号、类型、地点、等级、带时区的时间及 request_id"})
    return Handler


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True, type=Path)
    args = parser.parse_args()
    logging.basicConfig(level=logging.INFO, format="%(levelname)s %(message)s")
    harness = Harness(args.config)
    server = ThreadingHTTPServer(("127.0.0.1", int(harness.config.get("port", 9088))), handler_for(harness))
    server.daemon_threads = True
    server.timeout = 15
    LOG.info("推送调试工具：%s；仅测试账号，模拟事件", harness.http_origin)
    try:
        server.serve_forever()
    finally:
        harness.close()
        server.server_close()


if __name__ == "__main__":
    main()
