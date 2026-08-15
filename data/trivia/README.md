# Trivia import staging

`ro-choice.unreviewed.json` is a normalized staging bank generated from the Romanian CSV supplied by the project owner. The loader refuses it until every entry is marked `reviewed` (unless the explicit private-testing override is used).

Regenerate it and its audit report with:

```bash
python3 scripts/prepare-trivia-import.py /path/to/intrebari_cultura_generala.csv \
  --output data/trivia/ro-choice.unreviewed.json \
  --report data/trivia/ro-choice.audit.json
```

The converter removes a repeated question after its first occurrence, rejects malformed rows, normalizes Unicode and whitespace, generates stable opaque option IDs, and records a SHA-256 fingerprint of the input. It never changes the source CSV.

Before activation:

1. Confirm the dataset's redistribution license and required attribution.
2. Review Romanian wording and factual accuracy.
3. Decide whether unreviewed questions may be used or only explicitly approved entries.
4. Change the bank-level `reviewStatus` to `reviewed`, leaving any rejected item overrides as `unreviewed`, then load it:

```bash
make load-bank BANK_FILE=data/trivia/ro-choice.unreviewed.json
```

For private testing before review only:

```bash
make load-bank BANK_FILE=data/trivia/ro-choice.unreviewed.json ALLOW_UNREVIEWED=yes
```

Loading is transactional and idempotent. Reloading the same `bankName` disables items removed from the new file and upserts the current items. Played rounds remain unchanged because bank items are copied into the existing frozen round tables when selected.

## Bank format

Every file has `version`, a stable `bankName`, a default `reviewStatus`, and `entries`. Common entry fields are `id`, `gameKind`, `locale`, `category`, and `question`. An entry may override the bank-level review status.

- `trivia_choice` adds `canonicalAnswer`, exactly four `options`, `correctOptionId`, and `baseScore: 100`.
- `trivia_open` adds `canonicalAnswer`, optional `acceptedAliases`, and `baseScore: 100`; it has no options.
- `model_says` adds `answers`, each containing `canonicalAnswer`, optional `aliases`, a unique positive `rank`, and a positive `score`.

Optional trivia fields are `explanation` and `source`. Runtime integrity hashes are computed when content is frozen into a round.
