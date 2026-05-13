#!/usr/bin/env python3
"""Validate every derived file `mctf generate` wrote into the sample event
against its JSON Schema. This proves the Go generators produce schema-valid
output — closes the loop on the contract.
"""

import json
import sys
from pathlib import Path

import yaml
from jsonschema import Draft202012Validator

ROOT = Path(__file__).resolve().parents[1]
SCHEMA_DIR = ROOT / "schema"
EVENT = ROOT / "examples" / "sample-event"


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


def parse_toml(path: Path) -> dict:
    """Tiny TOML parser sufficient for our flat instance.toml format."""
    out: dict = {}
    section = out
    for raw in path.read_text().splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("[") and line.endswith("]"):
            key = line[1:-1]
            section = out.setdefault(key, {})
            continue
        if "=" not in line:
            continue
        k, _, v = line.partition("=")
        k, v = k.strip(), v.strip()
        if v.startswith('"') and v.endswith('"'):
            v = v[1:-1].encode().decode("unicode_escape")
        elif v.startswith("[") and v.endswith("]"):
            inner = v[1:-1].strip()
            v = [
                int(x) if x.lstrip("-").isdigit() else x.strip('"')
                for x in inner.split(",")
                if x.strip()
            ]
        elif v.lstrip("-").isdigit():
            v = int(v)
        section[k] = v
    return out


def main() -> int:
    rc = 0
    print("# generated CTFd manifests")
    for path in sorted(EVENT.glob("challenges/*/*/challenge.yml")):
        rc |= validate(
            str(path.relative_to(ROOT)),
            yaml.safe_load(path.read_text()),
            SCHEMA_DIR / "ctfd-manifest.schema.json",
        )

    print("\n# generated instancer configs")
    for path in sorted(EVENT.glob("challenges/*/*/instance.toml")):
        rc |= validate(
            str(path.relative_to(ROOT)),
            parse_toml(path),
            SCHEMA_DIR / "instance.schema.json",
        )

    return rc


if __name__ == "__main__":
    sys.exit(main())
