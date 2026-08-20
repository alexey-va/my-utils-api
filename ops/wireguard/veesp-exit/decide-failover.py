#!/usr/bin/env python3
import argparse
import json
from pathlib import Path


EXIT_IDS = ("primary", "secondary")


def positive_threshold(value: str) -> int:
    parsed = int(value)
    if parsed < 1 or parsed > 20:
        raise argparse.ArgumentTypeError("threshold must be between 1 and 20")
    return parsed


def read_object(path: str) -> dict:
    value = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError(f"{path} must contain a JSON object")
    return value


def counter(value: object) -> int:
    if not isinstance(value, int) or isinstance(value, bool) or value < 0:
        return 0
    return min(value, 1_000_000)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--state", required=True)
    parser.add_argument("--probes", required=True)
    parser.add_argument("--failure-threshold", required=True, type=positive_threshold)
    parser.add_argument("--recovery-threshold", required=True, type=positive_threshold)
    parser.add_argument("--preference", choices=("AUTO", "PRIMARY", "SECONDARY"), default="AUTO")
    args = parser.parse_args()

    state = read_object(args.state)
    probes = read_object(args.probes)
    previous_active = state.get("active")
    if previous_active not in (*EXIT_IDS, None):
        previous_active = None
    previous_counters = state.get("counters")
    if not isinstance(previous_counters, dict):
        previous_counters = {}

    counters: dict[str, dict[str, int]] = {}
    for exit_id in EXIT_IDS:
        probe = probes.get(exit_id)
        if not isinstance(probe, dict) or not isinstance(probe.get("healthy"), bool):
            raise ValueError(f"missing boolean probe health for {exit_id}")
        old = previous_counters.get(exit_id)
        if not isinstance(old, dict):
            old = {}
        if probe["healthy"]:
            successes = min(counter(old.get("successes")) + 1, 1_000_000)
            failures = 0
        else:
            successes = 0
            failures = min(counter(old.get("failures")) + 1, 1_000_000)
        counters[exit_id] = {"successes": successes, "failures": failures}

    primary_ready = counters["primary"]["successes"] >= args.recovery_threshold
    secondary_ready = counters["secondary"]["successes"] >= args.recovery_threshold
    primary_failed = counters["primary"]["failures"] >= args.failure_threshold
    secondary_failed = counters["secondary"]["failures"] >= args.failure_threshold

    if args.preference == "PRIMARY":
        active = "primary" if probes["primary"]["healthy"] else ("secondary" if probes["secondary"]["healthy"] else None)
    elif args.preference == "SECONDARY":
        active = "secondary" if probes["secondary"]["healthy"] else ("primary" if probes["primary"]["healthy"] else None)
    else:
        active = previous_active
        if active == "primary":
            if primary_failed:
                active = "secondary" if secondary_ready else None
        elif active == "secondary":
            if primary_ready:
                active = "primary"
            elif secondary_failed:
                active = None
        elif primary_ready:
            active = "primary"
        elif secondary_ready:
            active = "secondary"

    result = {
        "schemaVersion": 1,
        "active": active,
        "previousActive": previous_active,
        "preference": args.preference,
        "changed": active != previous_active,
        "counters": counters,
    }
    print(json.dumps(result, separators=(",", ":"), sort_keys=True))


if __name__ == "__main__":
    main()
