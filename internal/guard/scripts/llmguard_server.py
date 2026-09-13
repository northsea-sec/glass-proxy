#!/usr/bin/env python3
"""llmguard_server.py — minimal HTTP sidecar wrapping LLM Guard scanners.

Provides /scan endpoint that runs PromptInjection and InvisibleText scanners.
Designed to be started by the Go guard server as a managed subprocess.

Usage:
    python3 llmguard_server.py --bind 127.0.0.1:18901
"""

import argparse
import json
import sys
from http.server import HTTPServer, BaseHTTPRequestHandler

# Lazy-load LLM Guard to allow fast startup health checks
_scanners = None

def get_scanners():
    global _scanners
    if _scanners is None:
        try:
            from llm_guard.input_scanners import PromptInjection
            from llm_guard.input_scanners import InvisibleText
            _scanners = {
                "prompt_injection": PromptInjection(threshold=0.5),
                "invisible_text": InvisibleText(),
            }
        except ImportError as e:
            print(f"[LLMGUARD] Import error: {e}", file=sys.stderr)
            _scanners = {}
    return _scanners


class LLMGuardHandler(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        # Suppress default request logging
        pass

    def do_GET(self):
        if self.path == "/health":
            self._respond(200, {"status": "ok"})
            return
        self._respond(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/scan":
            self._respond(404, {"error": "not found"})
            return

        try:
            length = int(self.headers.get("Content-Length", 0))
            body = json.loads(self.rfile.read(length)) if length > 0 else {}
        except (json.JSONDecodeError, ValueError):
            self._respond(400, {"error": "invalid JSON"})
            return

        text = body.get("text", "")
        if not text.strip():
            self._respond(200, {"allowed": True, "reasons": []})
            return

        scanners = get_scanners()
        if not scanners:
            self._respond(200, {"allowed": True, "reasons": [], "warning": "no scanners available"})
            return

        reasons = []
        sanitized = text

        for name, scanner in scanners.items():
            try:
                result = scanner.scan(sanitized)
                # llm-guard >=0.3.16 returns (sanitized_output, is_valid, risk_score)
                # or a ScanResult namedtuple. Handle both.
                if isinstance(result, tuple):
                    if len(result) == 3:
                        sanitized_out, is_valid, risk_score = result
                    else:
                        sanitized_out, is_valid = result
                        risk_score = 0.0
                else:
                    sanitized_out = getattr(result, "sanitized_output", sanitized)
                    is_valid = getattr(result, "is_valid", True)
                    risk_score = getattr(result, "risk_score", 0.0)
                if not is_valid:
                    reasons.append(f"LLM Guard {name}: risk_score={risk_score:.3f}")
                    sanitized = sanitized_out
            except Exception as e:
                reasons.append(f"LLM Guard {name} error: {e}")

        self._respond(200, {
            "allowed": len(reasons) == 0,
            "reasons": reasons,
            "sanitized_text": sanitized if reasons else "",
        })

    def _respond(self, status, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--bind", default="127.0.0.1:18901")
    args = parser.parse_args()

    host, port = args.bind.rsplit(":", 1)
    server = HTTPServer((host, int(port)), LLMGuardHandler)
    print(f"[LLMGUARD] Listening on {args.bind}", file=sys.stderr)

    # Pre-warm scanners in background
    import threading
    threading.Thread(target=get_scanners, daemon=True).start()

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        server.shutdown()


if __name__ == "__main__":
    main()
