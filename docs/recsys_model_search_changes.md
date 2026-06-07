# Model-Based Search and Ranking Changes

## Goal

This branch adds a minimal model-based ranking loop that can run on the current
CPU-only host:

1. Collect ranking feedback through `feed_event` and `search_event`.
2. Build training rows from feedback, hole/floor metadata, tags, divisions, and
   feature snapshots.
3. Train small JSON linear rerankers with `cmd/recsys-train`.
4. Load those JSON models online for homepage recommendation and search rerank.
5. Fall back to rule ranking when a model is disabled, missing, invalid, or has
   an incompatible task.

The implementation is intentionally a lightweight learning-to-rank reranker, not
a placeholder architecture. It is cheap enough for production CPU inference and
keeps the training artifact inspectable.

## Why Existing Code Was Changed

### `models/feed_event.go` and `models/search_event.go`

These tables are the feedback log for training. `feed_event` stores homepage
exposures/actions. `search_event` stores search impressions/actions with query
hashes instead of raw query text. They are used to create pointwise labels such
as open/click/reply/favorite after an impression.

### `models/hole_feature.go` and `recsys/features_task.go`

`hole_feature` is the shared online/offline feature snapshot table. Ranking,
recall, and training can all reuse the same recent activity and quality scores.
This avoids recomputing expensive aggregates inside every request and gives
future algorithms a stable feature source.

### `recsys/`

The recommendation pipeline lives here so future ranking algorithms do not need
to be wired through every API handler:

```text
filter -> multi-recall -> rule/model ranking -> diversity reranking -> logging
```

New algorithms should normally plug into `recsys` as a ranker or recall source.
The existing `/apis` handlers should only pass request options into this module.

### `models/search_model.go` and `models/elastic.go`

Search has two recall paths:

```text
Elasticsearch lexical recall -> feedback fatigue -> model rerank
DB LIKE fallback             -> feedback fatigue -> model rerank
```

`models/elastic.go` already owns search recall, so the reranker is called there
to avoid duplicating search result shaping elsewhere. The DB fallback was changed
to scan `floor` by primary key with an `EXISTS` hidden-hole filter on MySQL,
because the old `hole_id IN (SELECT ...)` plan scanned `hole` first and used a
temporary filesort. On the imported data, the search SQL for `唱戏` dropped from
about 3.49s to about 0.21s.

### `recsys/ranking/sort.go`

The old `hot` and `recommend` score included absolute Unix timestamps. That made
the score mostly a timestamp score around 20000, while replies/favorites only
added hundreds or thousands. The new score uses bounded recency:

```text
recency = weight * 24 / (24 + age_hours)
```

This keeps recency useful without letting it dominate engagement. `hot` is now a
global engagement-heavy score. It only hard-filters hide/report feedback and no
longer demotes posts merely because the current user opened them. `recommend`
keeps personalized fatigue because that mode is explicitly recommendation-like.

### `cmd/recsys-train`

The trainer now supports `-bootstrap-cold-start`. This is only for imported
historical content that has little matching behavior log. It appends weak labels
from real hole/floor engagement without writing fake events into production
tables. Metrics include `bootstrap_cold_start`, `event_sample_count`, and
`bootstrap_sample_count`, so reviewers can distinguish this from real feedback
training.

### `models/user.go`: `GetCurrUserID`

Some logging and feedback paths only need a stable user ID, not the full user row
or a `SELECT ... FOR UPDATE`. `GetCurrUserID` avoids unnecessary user loading in
those paths and also handles the test-login token. When code needs permissions,
admin status, or user config, it still uses `GetCurrLoginUser`.

## Current Production Model Artifacts

The deployed compose mounts:

```text
/root/coding/treehole_sort_live/models -> /app/models
```

Current files:

```text
home_model.json
home_model.metrics.json
search_model.json
search_model.metrics.json
```

The treehole container is configured with:

```text
RECSYS_MODEL_RANKING=true
RECSYS_MODEL_PATH=/app/models/home_model.json
SEARCH_MODEL_RANKING=true
SEARCH_MODEL_PATH=/app/models/search_model.json
```

## Extension Rules

Future algorithms should avoid adding algorithm-specific branching to API
handlers. Prefer these integration points:

- Add recall logic under `recsys/` and expose it through `HomeFeedRequest`.
- Add online scoring through `recsys/modelrank` or a new ranker interface in
  `recsys`.
- Add reusable feature snapshots to `hole_feature` or a parallel feature table.
- Keep search reranking behind `applySearchModelRerank` or a replacement search
  ranker interface.
- Keep raw query text out of logs; store query hashes and derived query stats.

This keeps `/apis` and `/models` changes small when more algorithms are added.
