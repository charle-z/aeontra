#!/usr/bin/env python3
"""Validate and summarize direct Edge benchmark evidence."""

from __future__ import annotations

import json
import math
import sys
from pathlib import Path
from typing import Any


METRICS = (
    "queue_us",
    "pickup_us",
    "preflight_us",
    "execution_us",
    "result_us",
    "completion_us",
    "pickup_preflight_us",
    "total_us",
)


def percentile(values: list[float], fraction: float) -> float:
    ordered = sorted(values)
    if not ordered:
        raise ValueError("percentile requires at least one value")
    position = (len(ordered) - 1) * fraction
    lower = math.floor(position)
    upper = math.ceil(position)
    if lower == upper:
        return ordered[lower]
    return ordered[lower] + (ordered[upper] - ordered[lower]) * (position - lower)


def load(path: Path) -> dict[str, Any]:
    value = json.loads(path.read_text())
    if not isinstance(value, dict) or value.get("schema_version") != 1:
        raise ValueError("unsupported benchmark schema")
    samples = value.get("samples")
    if not isinstance(samples, list) or not samples:
        raise ValueError("benchmark samples are missing")
    return value


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: analyze-direct-edge-benchmark.py EVIDENCE.json", file=sys.stderr)
        return 2
    document = load(Path(sys.argv[1]))
    samples: list[dict[str, Any]] = document["samples"]
    summary: dict[str, dict[str, float]] = {}
    for metric in METRICS:
        values = [float(sample[metric]) for sample in samples if metric in sample]
        if not values:
            continue
        if any(value < 0 for value in values):
            raise ValueError(f"negative {metric}")
        summary[metric] = {
            "p50": round(percentile(values, 0.50), 3),
            "p90": round(percentile(values, 0.90), 3),
            "p95": round(percentile(values, 0.95), 3),
            "max": round(max(values), 3),
        }
    errors = sum(bool(sample.get("error")) for sample in samples)
    output = {
        "sample_count": len(samples),
        "error_count": errors,
        "error_rate": round(errors / len(samples), 6),
        "summary": summary,
    }
    print(json.dumps(output, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
