#!/usr/bin/env python3
"""Offline image integration check; requires only host Python 3 and Docker.

Never mounts host credentials, publishes ports, logs in, or requests inference.
"""
import json
import queue
import subprocess
import sys
import threading
import time
import uuid


def run(*args, check=True):
    return subprocess.run(["docker", *args], check=check, text=True,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE)


def main():
    image = sys.argv[1] if len(sys.argv) > 1 else "agent:codex-smoke"
    version = run("run", "--rm", "--network", "none", "--entrypoint",
                  "/usr/local/bin/codex", image, "--version").stdout.strip()
    assert version == "codex-cli 0.167.2", version
    config = json.loads(run("image", "inspect", image).stdout)[0]["Config"]
    assert config["User"] in ("nonroot:nonroot", "65532:65532"), config["User"]
    assert set(config.get("ExposedPorts", {})) == {"8080/tcp"}
    name = "codex-runtime-smoke-" + uuid.uuid4().hex
    command = ["docker", "run", "--rm", "-i", "--name", name,
               "--network", "none", "--read-only", "--tmpfs",
               "/codex-home:rw,noexec,nosuid,uid=65532,gid=65532,mode=0700",
               "-e", "CODEX_HOME=/codex-home", "--entrypoint",
               "/usr/local/bin/codex", image, "app-server", "--listen", "stdio://",
               "-c", 'cli_auth_credentials_store="file"']
    process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE, text=True, bufsize=1)
    assert process.stdin is not None and process.stdout is not None and process.stderr is not None
    stdin, stdout, stderr = process.stdin, process.stdout, process.stderr
    messages = queue.Queue()
    errors = []
    def read_stdout():
        for line in stdout:
            messages.put(json.loads(line))
    def read_stderr():
        errors.extend(stderr)
    threading.Thread(target=read_stdout, daemon=True).start()
    threading.Thread(target=read_stderr, daemon=True).start()
    def send(message):
        stdin.write(json.dumps(message) + "\n")
        stdin.flush()
    def rpc(identifier, method, params):
        send({"id": identifier, "method": method, "params": params})
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            message = messages.get(timeout=max(0.1, deadline - time.monotonic()))
            if message.get("id") == identifier:
                return message
        raise AssertionError("RPC timed out: " + method)
    try:
        initialized = rpc(1, "initialize", {"clientInfo": {
            "name": "agent_runtime_smoke", "version": "1"}})
        assert "result" in initialized, initialized
        send({"method": "initialized", "params": {}})
        account = rpc(2, "account/read", {"refreshToken": False})
        assert account.get("result", {}).get("account", "missing") is None, account
        limits = rpc(3, "account/rateLimits/read", {})
        assert "error" in limits, limits
        assert limits["error"]["message"] == "codex account authentication required to read rate limits", limits
        print(json.dumps({"version": version, "initialize": initialized,
                          "account/read": account, "account/rateLimits/read": limits}))
    finally:
        run("rm", "-f", name, check=False)
        process.communicate(timeout=15)
        if errors:
            print("Codex stderr (isolated signed-out run):\n" + "".join(errors), file=sys.stderr)
    app = "agent-health-smoke-" + uuid.uuid4().hex
    try:
        run("run", "-d", "--rm", "--name", app, "--network", "none",
            "-e", "SIWC_DATA_DIR=/data", "--mount", "type=volume,destination=/data", image)
        health = None
        for _ in range(30):
            health = run("exec", app, "/agent", "healthz", check=False)
            if health.returncode == 0:
                break
            time.sleep(1)
        assert health is not None and health.returncode == 0, health
        run("exec", app, "/everyoneagent", "healthz")
        print("PASS: /agent healthz and /everyoneagent healthz; no network, credentials, inference, or published ports")
    finally:
        run("rm", "-f", app, check=False)


if __name__ == "__main__":
    main()
