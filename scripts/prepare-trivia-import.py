#!/usr/bin/env python3
"""Validate and normalize a multiple-choice trivia CSV for ModelSays."""

from __future__ import annotations

import argparse
import csv
import hashlib
import json
import re
import sys
import unicodedata
from collections import Counter
from pathlib import Path

EXPECTED_COLUMNS = (
    "category", "question_number", "question", "option_a", "option_b",
    "option_c", "option_d", "source_option_markers", "correct_option",
    "correct_answer",
)
OPTION_MARKERS = "abcd"


def clean(value: str) -> str:
    return " ".join(unicodedata.normalize("NFC", value).strip().split())


def normalized_key(value: str) -> str:
    return clean(value).casefold()


def slug(value: str) -> str:
    ascii_value = unicodedata.normalize("NFKD", value).encode("ascii", "ignore").decode()
    return re.sub(r"[^a-z0-9]+", "-", ascii_value.lower()).strip("-") or "general"


def prepare(source: Path) -> tuple[list[dict], dict]:
    source_bytes = source.read_bytes()
    rows: list[dict] = []
    rejected: list[dict] = []
    duplicate_questions: list[dict] = []
    categories: Counter[str] = Counter()
    seen_questions: dict[str, tuple[int, str]] = {}

    with source.open("r", encoding="utf-8-sig", newline="") as stream:
        reader = csv.DictReader(stream)
        if tuple(reader.fieldnames or ()) != EXPECTED_COLUMNS:
            raise ValueError(f"unexpected columns: {reader.fieldnames!r}")
        for line_number, raw in enumerate(reader, start=2):
            values = {key: clean(raw.get(key, "")) for key in EXPECTED_COLUMNS}
            missing = [key for key in EXPECTED_COLUMNS if key != "source_option_markers" and not values[key]]
            marker = values["correct_option"].lower()
            options = [values[f"option_{name}"] for name in OPTION_MARKERS]
            reasons: list[str] = []
            if missing:
                reasons.append("missing fields: " + ", ".join(missing))
            if marker not in OPTION_MARKERS:
                reasons.append(f"invalid correct_option {marker!r}")
            if len({normalized_key(option) for option in options}) != 4:
                reasons.append("options are not distinct")
            if any(len(option) > 120 for option in options):
                reasons.append("option exceeds 120 characters")
            if len(values["correct_answer"]) > 120:
                reasons.append("answer exceeds 120 characters")
            if marker in OPTION_MARKERS and values[f"option_{marker}"] != values["correct_answer"]:
                reasons.append("correct_answer does not equal marked option")
            if reasons:
                rejected.append({"line": line_number, "questionNumber": values["question_number"], "reasons": reasons})
                continue

            question_key = normalized_key(values["question"])
            if question_key in seen_questions:
                original_line, original_answer = seen_questions[question_key]
                duplicate_questions.append({
                    "line": line_number,
                    "questionNumber": values["question_number"],
                    "duplicateOfLine": original_line,
                    "sameAnswer": normalized_key(original_answer) == normalized_key(values["correct_answer"]),
                    "originalAnswer": original_answer,
                    "duplicateAnswer": values["correct_answer"],
                })
                continue
            seen_questions[question_key] = (line_number, values["correct_answer"])

            category = values["category"]
            categories[category] += 1
            item_id = f"trivia-choice-ro-{slug(category)}-{int(values['question_number']):04d}"
            option_items = [
                {"id": f"{item_id}-o{index + 1}", "label": label}
                for index, label in enumerate(options)
            ]
            correct_index = OPTION_MARKERS.index(marker)
            rows.append({
                "id": item_id,
                "gameKind": "trivia_choice",
                "locale": "ro",
                "category": category,
                "question": values["question"],
                "canonicalAnswer": values["correct_answer"],
                "options": option_items,
                "correctOptionId": option_items[correct_index]["id"],
                "baseScore": 100,
            })

    duplicate_ids = [item_id for item_id, count in Counter(row["id"] for row in rows).items() if count > 1]
    if duplicate_ids:
        raise ValueError(f"generated duplicate IDs: {duplicate_ids[:5]}")
    report = {
        "sourceFile": source.name,
        "sourceSha256": hashlib.sha256(source_bytes).hexdigest(),
        "inputRows": len(rows) + len(rejected) + len(duplicate_questions),
        "preparedRows": len(rows),
        "reviewStatus": "unreviewed",
        "categories": dict(sorted(categories.items())),
        "rejectedRows": rejected,
        "duplicateQuestionsRemoved": duplicate_questions,
        "conflictingDuplicateAnswers": sum(not row["sameAnswer"] for row in duplicate_questions),
        "warnings": [
            "Confirm the source dataset license and attribution before distribution.",
            "Questions require Romanian-language factual and copy editing before being marked reviewed.",
        ],
    }
    return rows, report


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=Path)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args()
    try:
        rows, report = prepare(args.source)
    except (OSError, ValueError, csv.Error) as error:
        print(f"prepare trivia import: {error}", file=sys.stderr)
        return 1
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps({"version": 1, "bankName": "romanian-general-knowledge", "reviewStatus": "unreviewed", "entries": rows}, ensure_ascii=False, separators=(",", ":")) + "\n", encoding="utf-8")
    args.report.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"Prepared {report['preparedRows']} of {report['inputRows']} rows in {args.output}")
    print(f"Audit report: {args.report}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
