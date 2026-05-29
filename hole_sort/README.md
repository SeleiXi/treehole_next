# Hole sorting strategies

This top-level package keeps global hole ranking logic outside API handlers and
models so future rewrites can move or replace the whole module with minimal
call-site changes.

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
