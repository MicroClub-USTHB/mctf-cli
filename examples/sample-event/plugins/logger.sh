#!/bin/sh
# Example MCTF plugin: logs each lifecycle hook it receives.
#
# Protocol: newline-delimited JSON-RPC 2.0. The host writes one request object
# per line to our stdin; we write one response object per line to stdout.
# IMPORTANT: stdout carries ONLY protocol JSON — all human logging goes to a
# file (or stderr), never stdout.
#
# Config: pass {"config": {"file": "/path/to/audit.log"}} in the plugins entry;
# defaults to ./mctf-plugin.log under the event repo.
#
# This is deliberately dependency-light (sh + sed/grep) to show that a plugin
# can be written in any language.

logfile="./mctf-plugin.log"

# Minimal field extractor for a flat-ish JSON line: field <name> <json>
field() {
  printf '%s' "$2" | sed -n 's/.*"'"$1"'":"\([^"]*\)".*/\1/p'
}

while IFS= read -r line; do
  method=$(field method "$line")
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')

  case "$method" in
    initialize)
      # Pick up an optional log file from config.
      cfgfile=$(field file "$line")
      [ -n "$cfgfile" ] && logfile="$cfgfile"
      echo "[logger] initialized, logging to $logfile" >&2
      printf '{"jsonrpc":"2.0","id":%s,"result":{"name":"logger","hooks":["before_deploy","after_deploy","before_build","after_build","on_instance_spawn"]}}\n' "$id"
      ;;
    shutdown)
      exit 0
      ;;
    before_deploy|after_deploy|before_build|after_build|on_instance_spawn)
      echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) $method $line" >> "$logfile"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"ok":true}}\n' "$id"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
  esac
done
