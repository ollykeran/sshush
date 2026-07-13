#!/usr/bin/env python3
"""Emit module@version lines for govulncheck findings (min fixed versions)."""

from __future__ import annotations

import json
import sys


def main() -> int:
    if len(sys.argv) != 2:
        print(f"usage: {sys.argv[0]} <govulncheck.json>", file=sys.stderr)
        return 2

    with open(sys.argv[1], encoding="utf-8") as f:
        data = f.read()

    dec = json.JSONDecoder()
    idx = 0
    fixes: dict[str, str] = {}
    while idx < len(data):
        while idx < len(data) and data[idx].isspace():
            idx += 1
        if idx >= len(data):
            break
        obj, end = dec.raw_decode(data, idx)
        idx = end
        finding = obj.get("finding")
        if not finding:
            continue
        fixed = finding.get("fixed_version")
        trace = finding.get("trace") or []
        if not fixed or not trace:
            continue
        # Call stack ends at vulnerable module; module-only findings have one frame.
        module = trace[-1].get("module")
        if not module or module in ("stdlib", "github.com/ollykeran/sshush"):
            continue
        prev = fixes.get(module)
        if prev is None or fixed > prev:
            fixes[module] = fixed

    for module, fixed in sorted(fixes.items()):
        # Floor at the advisory fix; go may pick a newer compatible version
        # when several security bumps are applied together.
        print(f"{module}@>={fixed}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
