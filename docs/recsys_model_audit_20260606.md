# Recsys Model Audit 2026-06-06

Branch: `recsys-model-search-20260605`

## Current Evidence

Read-only production data access was verified from the Docker network through
`172.17.0.1:33069`. No post content or raw search query text was read.

Production feedback snapshot:

| Metric | Value |
| --- | ---: |
| `feed_event` rows | 502 |
| 7-day `feed_event` rows | 502 |
| 30-day `feed_event` rows | 502 |
| `hole_feature` rows | 20 |
| latest `hole_feature.updated_at` | 2026-06-06 00:32:46.593 |

Feedback distribution:

| Event | Rows | Users | Holes | First event | Last event |
| --- | ---: | ---: | ---: | --- | --- |
| impression | 478 | 2 | 20 | 2026-05-30 18:03:39.196 | 2026-06-06 00:18:15.491 |
| open | 17 | 1 | 15 | 2026-06-05 12:42:26.799 | 2026-06-05 18:53:34.608 |
| click | 7 | 1 | 7 | 2026-05-30 19:55:15.663 | 2026-05-31 02:07:51.788 |

`search_event` is not present on the currently deployed production schema, so
search learning-to-rank cannot yet be trained from real query-result impressions.

## Home Model Dry Run

Command shape:

```bash
go run ./cmd/recsys-train \
  -task=home \
  -db "$DB_URL" \
  -days=30 \
  -limit=5000 \
  -epochs=8 \
  -out /tmp/treehole_home_model.json \
  -metrics-out /tmp/treehole_home_metrics.json
```

Observed output:

| Metric | Value |
| --- | ---: |
| samples | 25 |
| train samples | 20 |
| eval samples | 5 |
| positive samples | 20 |
| negative samples | 5 |
| train model AUC | 1.000000 |
| train model logloss | 0.502209 |
| train model NDCG@10 | 1.000000 |
| train baseline NDCG@10 | 1.000000 |
| eval model logloss | 0.781711 |
| eval model NDCG@10 | 1.000000 |
| eval baseline NDCG@10 | 1.000000 |

The eval split contains only positive labels, so eval AUC is undefined and
eval NDCG@10 has no useful discrimination. The generated model artifact proves
that the trainer can consume real production feedback, but it is not reliable
enough to enable online model ranking.

Current trainer defaults now reject datasets that are too small, one-sided, or
too concentrated in a few query/session/user groups. Use `-allow-weak-data`
only for pipeline smoke tests; do not deploy a model produced from weak data.

## Judgment Against Mature Practice

Current implementation is a minimal, safe model-based loop:

- real feedback tables are used for training;
- search impressions are privacy-preserving once deployed;
- search actions are attributed by `request_id` without storing raw query text;
- online inference is deterministic and has config-gated fallback;
- offline training now reports time-split metrics and baseline comparisons.

It is not yet mature enough to turn on model ranking in production:

- search LTR has no real `search_event` impressions yet;
- feedback volume is too small and heavily skewed toward impressions;
- eval splits can lack negative examples;
- no counterfactual logging or exploration bucket exists;
- no online canary metrics compare model rank against rule rank;
- no embedding recall, two-tower retrieval, GBDT, or cross-encoder path has been
  validated against Treehole data.

## Safe Next Step

Deploy this branch only with model ranking disabled:

```text
RECSYS_MODEL_RANKING=false
SEARCH_MODEL_RANKING=false
SEARCH_EVENT_LOGGING=true
```

With `SEARCH_EVENT_LOGGING=true`, search result impressions write hashed query
metadata and result rank context. If a later open/reply/favorite/subscribe event
uses the same `request_id`, the service also writes a `search_event` action row
that reuses the hashed query context. The trainer gives these request-matched
actions priority over weaker time-window attribution.

For shadow validation against the production database, start a non-public
container with:

```text
DISABLE_BACKGROUND_TASKS=true
```

This still runs migrations and serves API requests, but avoids duplicate purge,
feature-refresh, and message-cleanup jobs while validating the new
`search_event` schema and request path.

Production image builds use the `production` build tag, which disables dev/test
SQLite initialization and removes `go-sqlite3`/cgo from the production
dependency graph. This is intended to make candidate image builds and shadow
validation reproducible on low-memory hosts.

After at least several days of `search_event` collection, rerun:

```bash
go run ./cmd/recsys-train -task=search -db "$DB_URL" -days=7 \
  -out /data/search_model.json -metrics-out /data/search_metrics.json
go run ./cmd/recsys-train -task=home -db "$DB_URL" -days=7 \
  -out /data/home_model.json -metrics-out /data/home_metrics.json
```

Only consider enabling model ranking when eval data includes both positives and
negatives and model NDCG@10/AUC improves over the baseline without degrading
latency or suppression behavior.
