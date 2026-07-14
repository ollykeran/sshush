#!/usr/bin/env python3
"""Run sshush benchmarks, compare raw crypto vs sshush wrappers."""

import argparse
import datetime
import re
import subprocess
from pathlib import Path

BENCH_PACKAGES = [
    "./internal/kdf/",
    "./internal/vault/",
    "./internal/agent/",
    "./internal/keys/",
    "./internal/openssh/",
]

BENCH_DIR = Path(__file__).resolve().parent.parent / "benchmarks"
ROOT_DIR = Path(__file__).resolve().parent.parent

# Pairs: (raw_name, sshush_name, label)
PAIRS = [
    ("BenchmarkArgon2IDKey_Raw", "BenchmarkDeriveKey_Sshush", "Argon2 KDF"),
    ("BenchmarkAESGCMEncrypt_Raw", "BenchmarkEncryptBlob_Sshush", "AES-256-GCM Encrypt"),
    ("BenchmarkAESGCMDecrypt_Raw", "BenchmarkDecryptBlob_Sshush", "AES-256-GCM Decrypt"),
    ("BenchmarkUnlockChain_Raw", "BenchmarkVaultUnlock_Sshush", "Vault Unlock Chain"),
    ("BenchmarkSignChain_Raw", "BenchmarkVaultSign_Sshush", "Vault Sign Chain"),
]


def run_cmd(cmd: list[str]) -> str:
    result = subprocess.run(cmd, capture_output=True, text=True, cwd=ROOT_DIR)
    return result.stdout + result.stderr


def get_git_info() -> dict[str, str]:
    def git(args: list[str]) -> str:
        return subprocess.run(
            ["git"] + args, capture_output=True, text=True, cwd=ROOT_DIR,
        ).stdout.strip()

    return {
        "branch": git(["rev-parse", "--abbrev-ref", "HEAD"]),
        "commit": git(["rev-parse", "--short", "HEAD"]),
        "commit_msg": git(["log", "-1", "--pretty=%s"]),
    }


def get_platform_info(raw: str) -> dict[str, str]:
    info = {}
    for line in raw.splitlines():
        for key in ("goos", "goarch", "cpu"):
            if line.startswith(key + ":"):
                info[key] = line.split(":", 1)[1].strip()
    return info


def strip_suffix(name: str) -> str:
    """Strip the -N (GOMAXPROCS) suffix from benchmark names."""
    return re.sub(r"-\d+$", "", name)


def parse_benchmarks(raw: str) -> dict[str, dict]:
    """Parse benchmarks into a dict keyed by base name (suffix stripped)."""
    benchmarks = {}
    for line in raw.splitlines():
        line = line.strip()
        if not line.startswith("Benchmark"):
            continue
        match = re.match(
            r"(Benchmark\S+)\s+(\d+)\s+([\d.]+)\s+(ns|µs|ms|s)/op\s+([\d.]+\s*[BKMGT]?i?B)/op\s+(\d+)\s+allocs/op",
            line,
        )
        if not match:
            continue
        full_name = match.group(1)
        base_name = strip_suffix(full_name)
        benchmarks[base_name] = {
            "name": full_name,
            "base": base_name,
            "iterations": int(match.group(2)),
            "time": match.group(3),
            "time_unit": match.group(4),
            "bytes": match.group(5),
            "allocs": match.group(6),
        }
    return benchmarks


def to_ns(b: dict) -> float:
    """Convert benchmark time to nanoseconds."""
    val = float(b["time"])
    unit = b["time_unit"]
    multipliers = {"ns": 1, "µs": 1_000, "ms": 1_000_000, "s": 1_000_000_000}
    return val * multipliers.get(unit, 1)


def format_time_ns(ns: float) -> str:
    """Format nanoseconds into a human-readable string."""
    if ns >= 1_000_000_000:
        return f"{ns / 1_000_000_000:.2f} s"
    if ns >= 1_000_000:
        return f"{ns / 1_000_000:.2f} ms"
    if ns >= 1_000:
        return f"{ns / 1_000:.2f} µs"
    return f"{ns:.0f} ns"


def format_overhead(pct: float) -> str:
    """Format overhead percentage with color indicator."""
    if pct < 1:
        return f"  ~0%"
    if pct < 5:
        return f" +{pct:.1f}%"
    if pct < 20:
        return f" +{pct:.1f}%"
    return f" +{pct:.0f}%"


def print_all(benchmarks: dict[str, dict]) -> None:
    """Print all benchmarks in a flat table."""
    if not benchmarks:
        print("  No benchmarks found.")
        return

    bs = sorted(benchmarks.values(), key=lambda b: b["base"])
    name_w = max(len(b["base"]) for b in bs)
    iter_w = max(len(f'{b["iterations"]:,}') for b in bs)
    time_w = max(len(f'{b["time"]} {b["time_unit"]}/op') for b in bs)
    bytes_w = max(len(f'{b["bytes"]}/op') for b in bs)
    allocs_w = max(len(f'{b["allocs"]} allocs/op') for b in bs)

    print()
    for b in bs:
        print(
            f'  {b["base"]:<{name_w}}  '
            f'{f"{b["iterations"]:,}":>{iter_w}}  '
            f'{f"{b["time"]} {b["time_unit"]}/op":>{time_w}}  '
            f'{f"{b["bytes"]}/op":>{bytes_w}}  '
            f'{f"{b["allocs"]} allocs/op":>{allocs_w}}'
        )


def print_comparison(benchmarks: dict[str, dict]) -> None:
    """Print raw vs sshush pairs with overhead."""
    if not benchmarks:
        return

    paired_bases = set()
    for raw_name, sshush_name, _ in PAIRS:
        paired_bases.add(strip_suffix(raw_name))
        paired_bases.add(strip_suffix(sshush_name))

    standalone = [n for n in benchmarks if n not in paired_bases]

    print("\n  RAW vs SSHUSH OVERHEAD")
    print("  " + "=" * 78)

    for raw_name, sshush_name, label in PAIRS:
        raw_base = strip_suffix(raw_name)
        sshush_base = strip_suffix(sshush_name)
        raw = benchmarks.get(raw_base)
        sshush = benchmarks.get(sshush_base)
        if not raw or not sshush:
            continue

        raw_ns = to_ns(raw)
        sshush_ns = to_ns(sshush)
        overhead = ((sshush_ns - raw_ns) / raw_ns) * 100 if raw_ns > 0 else 0

        print(f"\n  {label}")
        print(f'    {"RAW":<42} {format_time_ns(raw_ns):>12}  {raw["bytes"]:>10}/op  {raw["allocs"]:>3} allocs')
        print(f'    {"SSHUSH":<42} {format_time_ns(sshush_ns):>12}  {sshush["bytes"]:>10}/op  {sshush["allocs"]:>3} allocs')
        print(f'    {"OVERHEAD":<42} {format_overhead(overhead):>12}')

    if standalone:
        print(f"\n  STANDALONE (no raw comparison)")
        print("  " + "-" * 78)
        bs = [(n, benchmarks[n]) for n in sorted(standalone)]
        name_w = max(len(n) for n, _ in bs) if bs else 0
        for name, b in bs:
            print(
                f'    {name:<{name_w}}  '
                f'{f"{b["iterations"]:,}":>10}  '
                f'{f"{b["time"]} {b["time_unit"]}/op":>14}  '
                f'{f"{b["bytes"]}/op":>10}  '
                f'{f"{b["allocs"]} allocs/op":>12}'
            )


def format_file(benchmarks: dict[str, dict], platform: dict[str, str], git: dict[str, str]) -> str:
    lines = [
        f"goos: {platform.get('goos', '?')}",
        f"goarch: {platform.get('goarch', '?')}",
        f"cpu: {platform.get('cpu', '?')}",
        f"date: {datetime.datetime.now().strftime('%Y-%m-%d %H:%M:%S')}",
        f"branch: {git['branch']}",
        f"commit: {git['commit']}",
        f"commit_msg: {git['commit_msg']}",
        "",
        "ALL BENCHMARKS",
        "-" * 78,
    ]

    # Flat list
    bs = sorted(benchmarks.values(), key=lambda b: b["base"])
    if bs:
        name_w = max(len(b["base"]) for b in bs)
        iter_w = max(len(f'{b["iterations"]:,}') for b in bs)
        time_w = max(len(f'{b["time"]} {b["time_unit"]}/op') for b in bs)
        bytes_w = max(len(f'{b["bytes"]}/op') for b in bs)
        allocs_w = max(len(f'{b["allocs"]} allocs/op') for b in bs)
        for b in bs:
            lines.append(
                f'  {b["base"]:<{name_w}}  '
                f'{f"{b["iterations"]:,}":>{iter_w}}  '
                f'{f"{b["time"]} {b["time_unit"]}/op":>{time_w}}  '
                f'{f"{b["bytes"]}/op":>{bytes_w}}  '
                f'{f"{b["allocs"]} allocs/op":>{allocs_w}}'
            )

    # Comparison
    lines.append("")
    lines.append("RAW vs SSHUSH OVERHEAD")
    lines.append("=" * 78)

    for raw_name, sshush_name, label in PAIRS:
        raw = benchmarks.get(raw_name)
        sshush = benchmarks.get(sshush_name)
        if not raw or not sshush:
            continue
        raw_ns = to_ns(raw)
        sshush_ns = to_ns(sshush)
        overhead = ((sshush_ns - raw_ns) / raw_ns) * 100 if raw_ns > 0 else 0
        lines.append(f"\n  {label}")
        lines.append(f'    {"RAW":<42} {format_time_ns(raw_ns):>12}  {raw["bytes"]:>10}/op  {raw["allocs"]:>3} allocs')
        lines.append(f'    {"SSHUSH":<42} {format_time_ns(sshush_ns):>12}  {sshush["bytes"]:>10}/op  {sshush["allocs"]:>3} allocs')
        lines.append(f'    {"OVERHEAD":<42} {format_overhead(overhead):>12}')

    lines.append("")
    return "\n".join(lines)


def main() -> None:
    parser = argparse.ArgumentParser(description="Run sshush benchmarks")
    parser.add_argument("-w", "--write", action="store_true", help="Write to benchmarks/<timestamp>.txt")
    parser.add_argument("-p", "--package", default="./...", help="Go package pattern (default: ./...)")
    parser.add_argument("-c", "--count", type=int, default=1, help="Benchmark count (default: 1)")
    args = parser.parse_args()

    pkgs = BENCH_PACKAGES if args.package == "./..." else [args.package]

    all_benchmarks = {}
    raw_all = ""
    for pkg in pkgs:
        cmd = ["go", "test", "-bench=.", "-benchmem", f"-count={args.count}", "-run=^$", pkg]
        print(f"running: {' '.join(cmd)}")
        raw = run_cmd(cmd)
        raw_all += raw + "\n"
        all_benchmarks.update(parse_benchmarks(raw))

    platform = get_platform_info(raw_all)
    git = get_git_info()

    print(f"\ngoos: {platform.get('goos', '?')}")
    print(f"goarch: {platform.get('goarch', '?')}")
    print(f"cpu: {platform.get('cpu', '?')}")

    print_all(all_benchmarks)
    print_comparison(all_benchmarks)

    if args.write:
        BENCH_DIR.mkdir(exist_ok=True)
        ts = datetime.datetime.now().strftime("%Y%m%d-%H%M%S")
        out_path = BENCH_DIR / f"{ts}.txt"
        out_path.write_text(format_file(all_benchmarks, platform, git))
        print(f"\nwrote {out_path}")


if __name__ == "__main__":
    main()
