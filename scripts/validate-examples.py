#!/usr/bin/env python3
"""Validate every example challenge.yml against schema/challenge.schema.json,
and the sample mctf.config.yml against schema/mtcf-config.schema.json.

This is a Phase-0 sanity check — once the Go CLI ships, `mctf validate` will
do the same thing (and more, including event-wide cross-checks).
"""

import json
import sys
from pathlib import Path

import yaml
from jsonschema import Draft202012Validator

ROOT = Path(__file__).resolve().parents[1]
SCHEMA_DIR = ROOT / "schema"
EXAMPLES = ROOT / "examples" / "sample-event"


def load(path: Path):
    text = path.read_text()
    return yaml.safe_load(text)


def validate(label: str, instance, schema_path: Path) -> int:
    schema = json.loads(schema_path.read_text())
    validator = Draft202012Validator(schema)
    errs = sorted(validator.iter_errors(instance), key=lambda e: e.path)
    if not errs:
        print(f"  ok  {label}")
        return 0
    print(f"  FAIL  {label}")
    for e in errs:
        loc = "/".join(str(p) for p in e.absolute_path) or "<root>"
        print(f"    - {loc}: {e.message}")
    return 1


def main() -> int:
    rc = 0
    challenge_schema = SCHEMA_DIR / "challenge.schema.json"
    config_schema = SCHEMA_DIR / "mtcf-config.schema.json"

    print("# mctf.config.yml")
    rc |= validate(
        "examples/sample-event/mctf.config.yml",
        load(EXAMPLES / "mctf.config.yml"),
        config_schema,
    )

    print("\n# challenge manifests")
    challenges_root = EXAMPLES / "challenges"
    manifests = sorted(challenges_root.glob("*/*/challenge/challenge.yml"))
    if not manifests:
        print("  (no manifests found)")
        return 1
    for path in manifests:
        rel = path.relative_to(ROOT)
        rc |= validate(str(rel), load(path), challenge_schema)

    return rc


if __name__ == "__main__":
    sys.exit(main())
