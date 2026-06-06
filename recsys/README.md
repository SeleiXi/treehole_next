# Recsys

This top-level package keeps global hole ranking and homepage feed logic outside
API handlers and models so future rewrites can move or replace the whole module
with minimal call-site changes. The reusable score-based query ordering helpers
live in `recsys/ranking` to avoid import cycles with `models`.

Supported strategies:

- `original`: existing time-based order. `order=time_updated` ranks by latest
  reply/update, and `order=time_created` ranks by creation time.
- `hot`: engagement-heavy score for currently active discussions. It combines
  replies, views, favorites, subscriptions, and update recency.
- `recommend`: search/recommendation-style score that gives more weight to
  stronger user intent signals such as favorites/subscriptions while keeping
  creation and update recency in the ranking.

Score-based strategies expose `sort_score` in API responses and accept
`cursor_score` plus `cursor_id` for keyset pagination. This avoids missing or
duplicating items when infinite scrolling through a globally ranked list.

The recommendation homepage path (`order=recommend` or `feed_mode=recommend`)
uses a lightweight feed pipeline:

```text
filter -> multi-recall -> rule/model ranking -> diversity reranking -> impression log
```

The production default remains rule ranking. A model-based layer can be enabled
with `RECSYS_MODEL_RANKING=true` and `RECSYS_MODEL_PATH=/path/model.json`. If the
model path is empty, missing, or invalid, ranking falls back to the existing rule
score.

Search has a matching minimal learning-to-rank loop:

```text
ES/DB lexical recall -> feedback fatigue -> optional model rerank -> sanitized response -> search impression log
```

`search_event` stores query hash, query length/token count, source, base rank,
optional ES score, result floor/hole IDs, position, and request ID. It does not
store the raw search query. Search model reranking is disabled by default and can
be enabled with `SEARCH_MODEL_RANKING=true` and `SEARCH_MODEL_PATH=/path/model.json`.

Train the first linear reranker directly from production-like MySQL data:

```bash
go run ./cmd/recsys-train -task=search -db "$DB_URL" -days=30 -out /data/search_model.json
go run ./cmd/recsys-train -task=home -db "$DB_URL" -days=30 -out /data/home_model.json
```

The trainer builds pointwise logistic samples from real feedback:

- `search`: `search_event` impressions are labeled positive when the same user
  opens/clicks/replies/favorites/subscribes to the same hole shortly after the
  search impression.
- `home`: `feed_event` impressions/opens/replies/favorites/subscriptions become
  user-hole training rows, joined with `hole`, `hole_feature`, tags, and division
  metadata.

The JSON model is intentionally simple: an intercept plus feature weights. This
keeps online inference deterministic, cheap, and easy to roll back while leaving
room to replace the offline trainer with GBDT, a two-tower retriever, or a
cross-encoder reranker later. The trainer uses an older-to-newer time split,
stores feature normalization stats in `feature_stats`, and reports train/eval
logloss, AUC, and NDCG@10 against the existing baseline ranker. Use
`-metrics-out /path/metrics.json` to persist the evaluation summary alongside the
model artifact. By default, the trainer refuses to write a model when either the
training split or non-empty evaluation split has only positive or only negative
labels; use `-allow-weak-data` only for shadow smoke tests.
