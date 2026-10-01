#!/usr/bin/env python3
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DOC_ROOT = ROOT / "docs"
README = ROOT / "README.md"

LINK_RE = re.compile(r"\[[^\]]*\]\(([^)]+)\)")
SCHEME_RE = re.compile(r"^[a-zA-Z][a-zA-Z0-9+.-]*:")


def iter_markdown_files():
    yield README
    for p in sorted(DOC_ROOT.rglob("*.md")):
        if p.name == "README.md":
            continue
        yield p


def check_links(path: Path):
    errors = []
    text = path.read_text(encoding="utf-8")
    for m in LINK_RE.finditer(text):
        raw = m.group(1).strip()
        if not raw or raw.startswith("#"):
            continue
        if SCHEME_RE.match(raw):
            continue
        target = raw.split("#", 1)[0].strip()
        if not target:
            continue
        resolved = (path.parent / target).resolve()
        if not resolved.exists():
            errors.append(f"{path}: broken local link -> {raw}")
    return errors


def check_confidence_labels():
    required = {
        ROOT / "docs" / "current-repository-contract.md",
        ROOT / "docs" / "current-architecture.md",
        ROOT / "docs" / "historical-archive-boundary.md",
    }
    allowed = {
        "Verified in source",
        "Historical claim",
        "External reference",
    }
    errors = []
    for p in required:
        text = p.read_text(encoding="utf-8")
        if not any(label in text for label in allowed):
            errors.append(f"{p}: missing confidence label")
    return errors


def check_required_terms():
    p = ROOT / "docs" / "current-architecture.md"
    text = p.read_text(encoding="utf-8")
    required_terms = [
        "Anthropic",
        "Codex",
        "Gemini",
        "OpenAI-compatible",
        "OpenRouter",
    ]
    errors = []
    for term in required_terms:
        if term not in text:
            errors.append(f"{p}: missing required lane term '{term}'")
    return errors


def main():
    errors = []
    for md in iter_markdown_files():
        errors.extend(check_links(md))
    errors.extend(check_confidence_labels())
    errors.extend(check_required_terms())

    if errors:
        print("Documentation validation failed:", file=sys.stderr)
        for err in errors:
            print(f"- {err}", file=sys.stderr)
        return 1

    print("Documentation validation passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
