#!/usr/bin/env python3
"""Merge an mtls block into an existing authbridge config YAML.

Reads the operator-rendered config from stdin, writes the merged
config to stdout. Idempotent: re-running with the same mode produces
identical output.

Usage:
    cat existing-config.yaml | mtls-merge.py <mode>

Mode is "permissive" or "strict".
"""

import sys

import yaml


def main() -> None:
    if len(sys.argv) != 2 or sys.argv[1] not in ("permissive", "strict"):
        sys.stderr.write(
            "usage: mtls-merge.py <permissive|strict>\n"
        )
        sys.exit(1)
    mode = sys.argv[1]

    text = sys.stdin.read()
    if not text.strip():
        sys.stderr.write("mtls-merge: input was empty\n")
        sys.exit(1)

    cfg = yaml.safe_load(text)
    if not isinstance(cfg, dict):
        sys.stderr.write("mtls-merge: input did not parse as a YAML mapping\n")
        sys.exit(1)

    cfg["mtls"] = {"mode": mode}

    yaml.safe_dump(cfg, sys.stdout, sort_keys=False)


if __name__ == "__main__":
    main()
