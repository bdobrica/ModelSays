# ADR-008: Persist frozen trivia content as validated versioned JSON

- Status: Accepted
- Date: 2026-08-11
- Decision owners: Model Says maintainers
- Extends: ADR-001 and ADR-002

## Context

Open Trivia and Choice Trivia share question metadata, scoring value, explanation, and source, but their private solutions differ. Open Trivia needs a canonical answer and explicit aliases; Choice Trivia also needs four ordered opaque options and one correct option ID. The content is written and read as a whole, must remain frozen across restarts and replays, and must not be queried by individual solution field during gameplay.

## Decision

Store one versioned, validated JSONB payload on each trivia round, accompanied by a separate integrity-hash column. Version 1 contains the trivia kind, canonical answer, ordered aliases, base score, optional explanation/source, ordered choice options, and correct option ID. A SHA-256 hash over the canonical JSON representation of every content field is embedded in the payload and duplicated in the relational hash column. Loads reject unsupported versions, malformed boundaries, or either hash mismatch.

Choice content has exactly four non-empty options. IDs are unique and opaque, labels are distinct after the same Unicode-aware normalization used for answers, and the correct ID belongs to the frozen set. Open content contains no choice fields. Public answering projections expose only common metadata and, for Choice Trivia, ordered option IDs and labels. Solutions, aliases, explanation, and source appear only after the existing reveal boundary and in completed replays.

## Consequences

- JSONB preserves option and alias order and permits an additive version without a join-heavy subtype schema.
- PostgreSQL checks payload/hash presence as a pair; the application owns semantic validation and cryptographic verification.
- Legacy Model Says rounds retain null trivia columns and their prediction-board behavior.
- Provider generation and authoritative scoring consume this contract in later steps; this decision does not expose unfinished trivia in the UI.
- Rolling migration 000015 back removes only trivia payload/hash columns and is safe before trivia rounds are generated in production.
