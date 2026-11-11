"""Start the WhatsApp bridge on demand and stop it when this MCP server exits.

The bridge is only started the first time a WhatsApp tool is used. It is started without pairing
(no QR code), stops after WHATSAPP_IDLE_TIMEOUT without requests, and exits together with this
process. Set WHATSAPP_AUTOSTART=0 to manage the bridge yourself.
"""
import atexit
import fcntl
import http.client
import json
import os
import shutil
import signal
import subprocess
import sys
import socket
import time
from functools import partial

BRIDGE_DIR = os.path.realpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'whatsapp-bridge'))
STORE_DIR = os.path.join(BRIDGE_DIR, 'store')
BINARY = os.path.join(BRIDGE_DIR, 'bin', 'whatsapp-bridge')
LOG_FILE = os.path.join(STORE_DIR, 'bridge.log')
RESTRICTION_MARKER = os.path.join(STORE_DIR, 'ACCOUNT_RESTRICTED')
API_TOKEN_PATH = os.path.join(STORE_DIR, 'api_token')
# The bridge API is a Unix socket in the private store dir, so the token is never sent to another program
SOCKET_PATH = os.path.join(STORE_DIR, 'bridge.sock')

# Exit codes from the bridge (lifecycle.go / guard.go)
EXIT_RESTRICTED, EXIT_NOT_PAIRED, EXIT_ALREADY_RUNNING = 3, 4, 5

START_TIMEOUT = 45
# Skip the status check if the bridge was confirmed connected this recently
RECHECK_AFTER = 30

log = partial(print, file=sys.stderr)

_process = None
_last_ok = 0.0


class BridgeUnavailable(RuntimeError):
    pass


class _UnixConnection(http.client.HTTPConnection):
    def __init__(self, timeout):
        super().__init__("localhost", timeout=timeout)

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(SOCKET_PATH)


def call(path, payload, timeout=120):
    """POST payload as JSON to the bridge API. Returns (status code, body text).

    Raises BridgeUnavailable if the token is missing and OSError if the bridge is not listening.
    """
    try:
        with open(API_TOKEN_PATH) as f:
            token = f.read().strip()
    except FileNotFoundError:
        raise BridgeUnavailable(f"Bridge API token not found at {API_TOKEN_PATH}: start the WhatsApp bridge first")
    conn = _UnixConnection(timeout)
    try:
        conn.request("POST", "/api/" + path, body=json.dumps(payload),
                     headers={"Content-Type": "application/json", "Authorization": f"Bearer {token}"})
        response = conn.getresponse()
        return response.status, response.read().decode("utf-8", "replace")
    finally:
        conn.close()


def _status():
    """Return the bridge status dict, or None if no bridge is answering."""
    try:
        code, body = call("status", {}, timeout=2)
        return json.loads(body) if code == 200 else None
    except (OSError, BridgeUnavailable, http.client.HTTPException, ValueError):
        return None


def _find_go():
    for candidate in (shutil.which("go"), "/opt/homebrew/bin/go", "/usr/local/go/bin/go", "/usr/local/bin/go"):
        if candidate and os.access(candidate, os.X_OK):
            return candidate
    return None


def _build_if_stale():
    """Build the bridge binary if it is missing or older than its sources."""
    sources = [os.path.join(BRIDGE_DIR, f) for f in os.listdir(BRIDGE_DIR) if f.endswith(".go") or f in ("go.mod", "go.sum")]
    if os.path.exists(BINARY) and os.path.getmtime(BINARY) >= max(os.path.getmtime(s) for s in sources):
        return

    go = _find_go()
    if not go:
        raise BridgeUnavailable("The bridge binary needs to be built but Go was not found: run `go build -o bin/whatsapp-bridge .` in whatsapp-bridge")
    log("Building WhatsApp bridge...")
    result = subprocess.run([go, "build", "-o", BINARY, "."], cwd=BRIDGE_DIR, stdin=subprocess.DEVNULL,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=600)
    if result.returncode != 0:
        raise BridgeUnavailable(f"Failed to build the bridge:\n{result.stdout[-2000:]}")


def _log_tail(lines=15):
    try:
        with open(LOG_FILE, errors="replace") as f:
            return "".join(f.readlines()[-lines:])
    except OSError:
        return ""


def _spawn():
    global _process
    _build_if_stale()

    env = dict(os.environ, WHATSAPP_NO_PAIRING="1", WHATSAPP_EXIT_WITH_PARENT="1")
    env.setdefault("WHATSAPP_IDLE_TIMEOUT", "15m")
    os.makedirs(STORE_DIR, mode=0o700, exist_ok=True)
    log_fd = os.open(LOG_FILE, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
    try:
        # stdout must never be inherited: it is this server's MCP channel
        _process = subprocess.Popen([BINARY], cwd=BRIDGE_DIR, env=env, stdin=subprocess.DEVNULL, stdout=log_fd, stderr=log_fd)
    finally:
        os.close(log_fd)
    log(f"Started WhatsApp bridge (pid {_process.pid})")


def _wait_until_connected(spawned):
    deadline = time.time() + START_TIMEOUT
    while time.time() < deadline:
        status = _status()
        if status and status.get("connected"):
            return
        code = _process.poll() if spawned and _process else None
        if code is not None:
            if code == EXIT_RESTRICTED:
                raise BridgeUnavailable(f"WhatsApp restricted this account: the bridge will not start. See {RESTRICTION_MARKER}")
            if code == EXIT_NOT_PAIRED:
                raise BridgeUnavailable("The bridge is not linked to WhatsApp: run it once in a terminal to scan the QR code "
                                        f"(cd {BRIDGE_DIR} && go run .)")
            if code == EXIT_ALREADY_RUNNING:
                spawned = False  # another bridge holds the session: keep waiting for it
                continue
            raise BridgeUnavailable(f"The bridge exited with code {code}:\n{_log_tail()}")
        time.sleep(1)
    raise BridgeUnavailable(f"The bridge did not connect within {START_TIMEOUT}s:\n{_log_tail()}")


def ensure_running():
    """Make sure a connected bridge is running, starting one if needed."""
    global _last_ok
    if os.environ.get("WHATSAPP_AUTOSTART") == "0":
        return
    if os.path.exists(RESTRICTION_MARKER):
        raise BridgeUnavailable(f"WhatsApp restricted this account: the bridge will not start. See {RESTRICTION_MARKER}")
    if time.time() - _last_ok < RECHECK_AFTER and (_process is None or _process.poll() is None):
        return

    os.makedirs(STORE_DIR, mode=0o700, exist_ok=True)
    # Serialize starts between Claude sessions, so only one of them spawns a bridge
    with open(os.path.join(STORE_DIR, 'autostart.lock'), 'w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        status = _status()
        spawned = status is None
        if spawned:
            _spawn()
        _wait_until_connected(spawned)

    if spawned:
        # Give WhatsApp a moment to deliver messages received while the bridge was off
        time.sleep(3)
    _last_ok = time.time()


def _stop():
    if _process and _process.poll() is None:
        log("Stopping WhatsApp bridge")
        _process.terminate()
        try:
            _process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            _process.kill()


atexit.register(_stop)
# Make SIGTERM run atexit handlers too; the bridge also exits by itself if this process dies
signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
