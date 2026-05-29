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
filter -> multi-recall -> rule ranking -> diversity reranking -> impression log
```

The current implementation is intentionally rule-based. It establishes the
service boundary, event table, feature table, recall/rank/rerank modules, and
fallback behavior before introducing heavier ML models.
