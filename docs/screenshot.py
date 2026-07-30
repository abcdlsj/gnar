"""Regenerate the README screenshots from a real gnar run.

    cargo build --release
    uv run --with pyte --with requests docs/screenshot.py

Runs the release binary under a pseudo-terminal, replays its output into a
terminal emulator, and writes the resulting cells as SVG.
"""

import fcntl
import http.server
import json
import os
import pty
import socketserver
import struct
import subprocess
import termios
import threading
import time
import unicodedata

BINARY = "target/release/gnar"
COLS, ROWS = 84, 24
EDGE_PORT, UPSTREAM_PORT = 8980, 8981

PALETTE = {
    "red": "#ff7b72",
    "green": "#7ee787",
    "brightgreen": "#7ee787",
    "yellow": "#e3b341",
    "blue": "#79c0ff",
    "cyan": "#79c0ff",
    "magenta": "#d2a8ff",
    "black": "#484f58",
    "brightblack": "#7d8590",
}
FOREGROUND, BACKGROUND = "#e6edf3", "#0d1117"
CELL_W, LINE_H, PAD, CHROME = 8.4, 19.0, 16.0, 28.0


def colour(name):
    if name in PALETTE:
        return PALETTE[name]
    value = name.removeprefix("#")
    if len(value) == 6:
        try:
            int(value, 16)
        except ValueError:
            return FOREGROUND
        return "#7d8590" if value.lower() == "0d1117" else f"#{value}"
    return FOREGROUND


def escape(text):
    return text.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


def render(cells, path, title):
    lines = len(cells)
    while lines > 1 and not any(cell["c"].strip() for cell in cells[lines - 1]):
        lines -= 1
    columns = len(cells[0])
    width = columns * CELL_W + PAD * 2
    height = lines * LINE_H + PAD * 2 + CHROME

    svg = [
        f'<svg xmlns="http://www.w3.org/2000/svg" width="{width:.0f}" '
        f'height="{height:.0f}" viewBox="0 0 {width:.0f} {height:.0f}" '
        'font-family="ui-monospace,SFMono-Regular,Menlo,Consolas,monospace" font-size="13">',
        f'<rect width="{width:.0f}" height="{height:.0f}" rx="8" fill="{BACKGROUND}"/>',
    ]
    for index, dot in enumerate(("#ff5f57", "#febc2e", "#28c840")):
        svg.append(f'<circle cx="{PAD + 6 + index * 15:.0f}" cy="16" r="5.5" fill="{dot}"/>')
    svg.append(
        f'<text x="{width / 2:.0f}" y="20" fill="#7d8590" font-size="11" '
        f'text-anchor="middle">{escape(title)}</text>'
    )

    for y in range(lines):
        row, x = cells[y], 0
        while x < columns:
            background = row[x].get("bg", "default")
            if background == "default":
                x += 1
                continue
            start = x
            while x < columns and row[x].get("bg", "default") == background:
                x += 1
            fill = f"#{background}" if len(background) == 6 else "#202730"
            svg.append(
                f'<rect x="{PAD + start * CELL_W:.1f}" y="{PAD + CHROME + y * LINE_H:.1f}" '
                f'width="{(x - start) * CELL_W:.1f}" height="{LINE_H:.1f}" fill="{fill}"/>'
            )

    for y in range(lines):
        row, x = cells[y], 0
        baseline = PAD + CHROME + y * LINE_H + 13
        while x < columns:
            if not row[x]["c"].strip():
                x += 1
                continue
            fill, bold = colour(row[x]["fg"]), row[x]["bold"]
            start, run = x, []
            while x < columns:
                cell = row[x]
                if colour(cell["fg"]) != fill or cell["bold"] != bold:
                    break
                if not cell["c"]:
                    if run and unicodedata.east_asian_width(run[-1]) in ("W", "F"):
                        x += 1
                        continue
                    break
                run.append(cell["c"])
                x += 1
            text = "".join(run).rstrip()
            if text:
                weight = ' font-weight="700"' if bold else ""
                svg.append(
                    f'<text x="{PAD + start * CELL_W:.1f}" y="{baseline:.0f}" fill="{fill}"'
                    f'{weight} xml:space="preserve">{escape(text)}</text>'
                )

    svg.append("</svg>")
    open(path, "w").write("\n".join(svg) + "\n")
    print(f"wrote {path}")


def capture(args, interact, columns=COLS, rows=ROWS):
    import pyte

    # Size the terminal before exec: ratatui lays out once at startup and then
    # sends only differences, so a later resize desynchronises the replay.
    controller, follower = pty.openpty()
    fcntl.ioctl(follower, termios.TIOCSWINSZ, struct.pack("HHHH", rows, columns, 0, 0))
    pid = os.fork()
    if pid == 0:
        os.setsid()
        fcntl.ioctl(follower, termios.TIOCSCTTY, 0)
        for target in (0, 1, 2):
            os.dup2(follower, target)
        os.close(controller)
        os.close(follower)
        environment = os.environ.copy()
        environment.pop("NO_COLOR", None)
        os.execve(BINARY, [BINARY, *args], environment)
    os.close(follower)
    fd = controller
    os.set_blocking(fd, False)

    captured = bytearray()
    draining = True

    def drain():
        try:
            while chunk := os.read(fd, 65536):
                captured.extend(chunk)
        except (BlockingIOError, OSError):
            pass

    # Consume continuously. The inspector redraws every 50ms, so reading only
    # between requests fills the pty buffer and blocks the client mid-render,
    # which shows up as absurd request durations.
    def pump():
        while draining:
            drain()
            time.sleep(0.005)

    reader = threading.Thread(target=pump, daemon=True)
    reader.start()
    try:
        interact(drain)
    finally:
        draining = False
        reader.join(timeout=1)
        try:
            os.kill(pid, 9)
        except OSError:
            pass

    screen = pyte.Screen(columns, rows)
    pyte.Stream(screen).feed(captured.decode("utf8", "replace"))
    return [
        [
            {
                "c": screen.buffer[y][x].data,
                "fg": screen.buffer[y][x].fg,
                "bg": screen.buffer[y][x].bg,
                "bold": screen.buffer[y][x].bold,
            }
            for x in range(columns)
        ]
        for y in range(rows)
    ]


class Upstream(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    def reply(self, status, body):
        payload = body.encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def do_GET(self):
        if self.path.startswith("/api/users"):
            self.reply(200, '{"users":[{"id":1,"name":"ada"}]}')
        elif self.path.startswith("/health") or self.path == "/":
            self.reply(200, '{"ok":true}')
        else:
            self.reply(404, '{"error":"not found"}')

    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length", 0)))
        self.reply(500, '{"error":"webhook handler crashed"}')


def shoot_discovery():
    def interact(drain):
        time.sleep(3.0)
        drain()

    cells = capture(["--edge", f"http://127.0.0.1:{EDGE_PORT}"], interact, rows=14)
    render(cells, "docs/discover.svg", "gnar — discover and identify")


def shoot_inspector():
    import requests

    socketserver.TCPServer.allow_reuse_address = True
    upstream = socketserver.ThreadingTCPServer(("127.0.0.1", UPSTREAM_PORT), Upstream)
    threading.Thread(target=upstream.serve_forever, daemon=True).start()
    edge = subprocess.Popen(
        [
            BINARY, "serve",
            "--listen", f"127.0.0.1:{EDGE_PORT}",
            "--public-url", f"http://127.0.0.1:{EDGE_PORT}",
            "--database", "/tmp/gnar-screenshot.db",
            "--anonymous-only",
        ],
        stdin=subprocess.DEVNULL,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    time.sleep(1.5)

    def interact(drain):
        time.sleep(2.5)
        drain()
        base = f"http://127.0.0.1:{EDGE_PORT}/t/warm-panda-42"
        traffic = [
            ("get", "/api/users"),
            ("post", "/webhooks/github"),
            ("get", "/health"),
            ("get", "/api/users"),
        ]
        session = requests.Session()
        for method, path in traffic:
            try:
                getattr(session, method)(
                    base + path, timeout=3, data="{}" if method == "post" else None
                )
            except requests.RequestException:
                pass
            time.sleep(0.25)
            drain()
        time.sleep(0.4)
        drain()

    try:
        cells = capture(
            [
                str(UPSTREAM_PORT),
                "--edge", f"http://127.0.0.1:{EDGE_PORT}",
                "--name", "warm-panda-42",
            ],
            interact,
        )
    finally:
        edge.terminate()
        upstream.shutdown()
        for suffix in ("", "-wal", "-shm"):
            try:
                os.remove(f"/tmp/gnar-screenshot.db{suffix}")
            except OSError:
                pass

    render(cells, "docs/inspect.svg", "gnar — request inspector")


if __name__ == "__main__":
    if not os.path.exists(BINARY):
        raise SystemExit(f"{BINARY} is missing; run `cargo build --release` first")
    shoot_inspector()
    shoot_discovery()
