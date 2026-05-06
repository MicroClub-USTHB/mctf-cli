#!/usr/bin/env python3
"""Hand-translate the cub3-clone author manifest into both derived files
and validate them against the derived-file schemas.

This proves the schemas are mutually self-consistent — when the future Go CLI
generates these files, they'll have a known-good shape to target.
"""
import json
import sys
from pathlib import Path

from jsonschema import Draft202012Validator

ROOT = Path(__file__).resolve().parents[1]
SCHEMA_DIR = ROOT / "schema"


def validate(label: str, instance, schema_path: Path) -> int:
    schema = json.loads(schema_path.read_text())
    errs = sorted(
        Draft202012Validator(schema).iter_errors(instance),
        key=lambda e: e.path,
    )
    if not errs:
        print(f"  ok  {label}")
        return 0
    print(f"  FAIL  {label}")
    for e in errs:
        loc = "/".join(str(p) for p in e.absolute_path) or "<root>"
        print(f"    - {loc}: {e.message}")
    return 1


# Derived CTFd manifest for cub3-clone (what `mtcf generate` would emit)
ctfd_manifest = {
    "name": "cub3-clone",
    "description": (
        "Can you solve a randomly scrambled Rubik's cube?\n\n"
        "Each team gets its own instance — solve speed matters.\n\n"
        "**Author: $h3rL0ck**"
    ),
    "author": "$h3rL0ck",
    "category": "web",
    "value": None,
    "type": "dynamic",
    "extra": {"initial": 500, "decay": 10, "minimum": 100},
    "flags": [
        {
            "type": "static",
            "content": "mctf{k3y5tr0k35_b4tt13_w0n}",
            "case_sensitive": "case_sensitive",
        }
    ],
    "tags": ["wave 1", "js", "client-side"],
    "connection_info": "https://instancer.ctf.example.com",
    "files": None,
    "state": "visible",
}

# Derived instancer config for cub3-clone (same source manifest)
instance_config = {
    "id": "cub3-clone",
    "name": "cub3-clone",
    "category": "web",
    "description": "Can you solve a randomly scrambled Rubik's cube?",
    "type": "http",
    "ports": [8000],
    "timeout": 1800,
    "extend_time": 900,
    "max_per_team": 1,
    "image": "ghcr.io/example/sample-event-web-cub3-clone:abc1234",
    "resources": {"cpu": "0.5", "memory": "256M"},
}


def main() -> int:
    rc = 0
    print("# derived: CTFd manifest (cub3-clone)")
    rc |= validate(
        "ctfd-manifest schema",
        ctfd_manifest,
        SCHEMA_DIR / "ctfd-manifest.schema.json",
    )

    print("\n# derived: instancer config (cub3-clone)")
    rc |= validate(
        "instance schema",
        instance_config,
        SCHEMA_DIR / "instance.schema.json",
    )
    return rc


if __name__ == "__main__":
    sys.exit(main())
