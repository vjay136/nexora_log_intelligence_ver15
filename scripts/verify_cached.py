#!/usr/bin/env python3
"""Run NLI verification once per source fingerprint and save reusable evidence.

The runner intentionally keeps quick, full, and release work separate. A
successful suite is skipped while every relevant source byte is unchanged.
Pass --force only when the environment, rather than the source, changed.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time


RUNNER_VERSION = "2"
SOURCE_DIRS = (
    ".github", "api", "cmd", "configs", "docs", "functions", "internal",
    "packaging", "parser-policy", "projection", "scripts", "static-layout",
    "supabase", "telemetry", "testdata", "tests", "web",
)
SOURCE_FILES = (
    ".env.local.example", ".gitattributes", ".gitignore", "CHANGELOG.md",
    "README.md", "go.mod", "go.sum", "Makefile", "package.json",
    "package-lock.json", "vercel.json",
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--suite", choices=("quick", "full", "release"), default="quick")
    parser.add_argument("--output-dir", type=Path)
    parser.add_argument("--node", help="node executable; defaults to NLI_NODE or PATH")
    parser.add_argument("--docker", default=os.environ.get("NLI_DOCKER", "docker"))
    parser.add_argument("--force", action="store_true")
    return parser.parse_args()


def source_paths(root: Path) -> list[Path]:
    paths: list[Path] = []
    for name in SOURCE_FILES:
        candidate = root / name
        if candidate.is_file():
            paths.append(candidate)
    for name in SOURCE_DIRS:
        directory = root / name
        if not directory.is_dir():
            continue
        for candidate in directory.rglob("*"):
            if any(part in {"node_modules", "build", "dist", "__pycache__", ".playwright"} for part in candidate.parts):
                continue
            if not candidate.is_file():
                continue
            paths.append(candidate)
    return sorted(set(paths), key=lambda value: value.relative_to(root).as_posix())


def fingerprint(root: Path, suite: str) -> tuple[str, int]:
    digest = hashlib.sha256()
    # Suite is deliberately excluded: quick/full/release dependency results
    # share one source fingerprint and can reuse earlier successful gates.
    digest.update(f"nli-verify:{RUNNER_VERSION}\0".encode())
    paths = source_paths(root)
    for path in paths:
        relative = path.relative_to(root).as_posix().encode("utf-8")
        digest.update(relative + b"\0")
        with path.open("rb") as stream:
            while block := stream.read(1024 * 1024):
                digest.update(block)
        digest.update(b"\0")
    return digest.hexdigest(), len(paths)


def resolve_node(requested: str | None) -> str:
    if requested:
        return requested
    configured = os.environ.get("NLI_NODE")
    if configured:
        return configured
    found = shutil.which("node")
    if found:
        return found
    bundled = Path.home() / ".cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node.exe"
    if bundled.is_file():
        return str(bundled)
    raise SystemExit("node was not found; pass --node PATH or set NLI_NODE")


def read_env(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if line and not line.startswith("#") and "=" in line:
            key, value = line.split("=", 1)
            values[key] = value
    return values


def run_step(name: str, command: list[str], cwd: Path, log) -> dict[str, object]:
    started = time.monotonic()
    log.write(f"\n===== {name} =====\n$ {subprocess.list2cmdline(command)}\n")
    log.flush()
    process = subprocess.Popen(
        command, cwd=cwd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        text=True, encoding="utf-8", errors="replace",
    )
    assert process.stdout is not None
    for line in process.stdout:
        try:
            sys.stdout.write(line)
        except UnicodeEncodeError:
            # Windows consoles may still use cp1252 while test tools emit
            # Unicode status glyphs. Preserve the evidence log and keep the
            # verification process running with a readable console fallback.
            encoding = getattr(sys.stdout, "encoding", None) or "ascii"
            sys.stdout.write(line.encode(encoding, errors="replace").decode(encoding))
        log.write(line)
    return_code = process.wait()
    elapsed = round(time.monotonic() - started, 3)
    log.write(f"===== {name}: {'PASS' if return_code == 0 else 'FAIL'} ({elapsed}s) =====\n")
    log.flush()
    return {"name": name, "command": command, "exit_code": return_code, "duration_seconds": elapsed}


def quick_steps(root: Path, node: str) -> list[tuple[str, list[str]]]:
    return [
        ("quick static release invariants", [sys.executable, str(root / "tests/release/quick_static_test.py"), str(root)]),
        ("client installer configuration", [sys.executable, str(root / "tests/release/client_installer_config_test.py"), str(root)]),
        ("source parser provenance", [sys.executable, str(root / "tests/release/source_parser_provenance_test.py"), str(root)]),
        ("web application syntax", [node, "--check", "web/app.js"]),
        ("Vercel proxy syntax", [node, "--check", "api/v1/[...path].js"]),
        ("Cloudflare proxy syntax", [node, "--check", "functions/api/v1/[[path]].js"]),
        ("web proxy contracts", [node, "--test", "tests/vercel-proxy.test.js", "tests/cloudflare-proxy.test.js"]),
        ("production web build", [node, "scripts/build-webapp.mjs"]),
    ]


def docker_go_step(root: Path, docker: str, image: str) -> tuple[str, list[str]]:
    root_mount = f"type=bind,src={root},dst=/src,readonly"
    check = (
        "set -euo pipefail; export PATH=/usr/local/go/bin:/usr/bin:/bin; "
        "test -z \"$(gofmt -l $(find . -name '*.go' -not -path './.git/*'))\"; "
        "go test ./...; go vet ./...; go test -race ./...; "
        "go test ./internal/mixedkv -run='^$' -fuzz=FuzzParseNeverPanics -fuzztime=5s"
    )
    return (
        "Go format, unit, vet, race, and fuzz",
        [docker, "run", "--rm", "--mount", root_mount,
         "--mount", "type=volume,src=nli-go-build-cache,dst=/root/.cache/go-build",
         "--mount", "type=volume,src=nli-go-mod-cache,dst=/go/pkg/mod",
         "--workdir", "/src", image, "bash", "-lc", check],
    )


def main() -> int:
    args = parse_args()
    root = args.root.resolve()
    if not (root / "go.mod").is_file():
        raise SystemExit(f"not an NLI source root: {root}")
    output_dir = (args.output_dir or (root.parent / "_nli-verification")).resolve()
    output_dir.mkdir(parents=True, exist_ok=True)
    digest, file_count = fingerprint(root, args.suite)
    short = digest[:16]
    result_path = output_dir / f"{args.suite}-{short}.result.json"
    log_path = output_dir / f"{args.suite}-{short}.log"
    latest_path = output_dir / f"latest-{args.suite}.json"
    if not args.force and result_path.is_file() and log_path.is_file():
        try:
            prior = json.loads(result_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            prior = {}
        if (
            prior.get("status") == "passed"
            and prior.get("suite") == args.suite
            and prior.get("fingerprint") == digest
            and prior.get("source_file_count") == file_count
            and prior.get("runner_version") == RUNNER_VERSION
        ):
            print(f"CACHE HIT: {args.suite} already passed for {short}")
            print(f"Result: {result_path}")
            print(f"Log:    {log_path}")
            return 0

    def passed(suite: str) -> bool:
        candidate = output_dir / f"{suite}-{short}.result.json"
        if not candidate.is_file():
            return False
        try:
            return json.loads(candidate.read_text(encoding="utf-8")).get("status") == "passed"
        except (OSError, ValueError):
            return False

    node = resolve_node(args.node)
    quick_cached = passed("quick")
    full_cached = passed("full")
    steps: list[tuple[str, list[str]]] = []
    if args.suite == "quick" or not quick_cached:
        steps.extend(quick_steps(root, node))
    images = read_env(root / "packaging/docker/images.env")
    # Both the browser gate and the release builder require a session-owned
    # scratch root. Set it for full and release suites even when the full gate
    # is satisfied by a content-hash cache hit.
    if args.suite in {"full", "release"}:
        os.environ.setdefault("NLI_BUILD_SCRATCH", str(output_dir / "scratch"))
    if args.suite in {"full", "release"} and not full_cached:
        steps.append(docker_go_step(root, args.docker, images["GO_IMAGE"]))
        if os.name == "nt":
            browser_command = ["powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File",
                               str(root / "scripts/run-webview-tests.ps1"), "-SkipGoGate"]
        else:
            os.environ["NLI_SKIP_GO_GATE"] = "1"
            browser_command = ["bash", str(root / "scripts/run-webview-tests.sh")]
        steps.append(("authenticated browser-to-controller flows", browser_command))
    if args.suite == "release":
        release_dir = output_dir / f"release-{short}"
        if release_dir.exists() and any(release_dir.iterdir()):
            raise SystemExit(f"release output is not empty: {release_dir}")
        if os.name == "nt":
            release_command = ["powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File",
                               str(root / "packaging/build-release.ps1"), "-OutputDirectory", str(release_dir)]
        else:
            release_command = ["bash", str(root / "scripts/release/build.sh"), str(release_dir)]
        steps.append((
            "reproducible RPM and release artifacts",
            release_command,
        ))

    started_at = dt.datetime.now(dt.timezone.utc)
    results: list[dict[str, object]] = []
    status = "passed"
    with log_path.open("w", encoding="utf-8", newline="\n") as log:
        log.write(f"NLI cached verification\nroot={root}\nsuite={args.suite}\nfingerprint={digest}\nfiles={file_count}\n")
        for name, command in steps:
            result = run_step(name, command, root, log)
            results.append(result)
            if result["exit_code"] != 0:
                status = "failed"
                break
    document = {
        "schema_version": 1,
        "runner_version": RUNNER_VERSION,
        "status": status,
        "suite": args.suite,
        "fingerprint": digest,
        "source_file_count": file_count,
        "root": str(root),
        "started_at": started_at.isoformat(),
        "completed_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "steps": results,
        "log": str(log_path),
    }
    result_path.write_text(json.dumps(document, indent=2) + "\n", encoding="utf-8")
    latest_path.write_text(json.dumps(document, indent=2) + "\n", encoding="utf-8")
    print(f"{args.suite.upper()} SUITE: {status.upper()}")
    print(f"Result: {result_path}")
    print(f"Log:    {log_path}")
    return 0 if status == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
