---
date: 2026-02-09
type: dashboard
domain: personal
status: active
tags: []
summary: "Token usage, friction trends, tool usage, and project analytics"
---

# Analytics Dashboard

## Token Usage

### Recent Sessions

```dataview
TABLE
  tokens_in AS "In",
  tokens_out AS "Out",
  (tokens_in + tokens_out) AS "Total",
  duration_minutes AS "Min",
  project AS "Project"
FROM "Projects"
WHERE type = "session" AND tokens_in != null
SORT date DESC, iteration DESC
LIMIT 30
```

### Weekly Token Totals

```dataview
TABLE
  length(rows) AS "Sessions",
  sum(rows.tokens_in) AS "Tokens In",
  sum(rows.tokens_out) AS "Tokens Out",
  (sum(rows.tokens_in) + sum(rows.tokens_out)) AS "Total"
FROM "Projects"
WHERE type = "session" AND tokens_in != null
GROUP BY dateformat(date, "yyyy-'W'WW") AS "Week"
SORT key DESC
LIMIT 12
```

### Per-Project Token Totals

```dataview
TABLE
  length(rows) AS "Sessions",
  sum(rows.tokens_in) AS "Tokens In",
  sum(rows.tokens_out) AS "Tokens Out",
  (sum(rows.tokens_in) + sum(rows.tokens_out)) AS "Total"
FROM "Projects"
WHERE type = "session" AND tokens_in != null AND project != null AND project != ""
GROUP BY project
SORT sum(rows.tokens_in) + sum(rows.tokens_out) DESC
```

## Friction Trends

### High-Friction Sessions

```dataview
TABLE
  date AS "Date",
  project AS "Project",
  friction_score AS "Friction",
  corrections AS "Corrections",
  summary AS "Summary"
FROM "Projects"
WHERE type = "session" AND friction_score != null AND friction_score > 0
SORT friction_score DESC
LIMIT 20
```

### Per-Project Average Friction

```dataview
TABLE
  length(rows) AS "Sessions",
  round(sum(rows.friction_score) / length(rows)) AS "Avg Friction",
  max(rows.friction_score) AS "Max Friction",
  sum(rows.corrections) AS "Corrections"
FROM "Projects"
WHERE type = "session" AND friction_score != null AND friction_score > 0 AND project != null AND project != ""
GROUP BY project
SORT round(sum(rows.friction_score) / length(rows)) DESC
```

## Tool Usage

### Tool Frequency

```dataview
TABLE
  length(rows) AS "Uses"
FROM "Projects"
WHERE type = "session" AND tools != null
FLATTEN tools AS tool
GROUP BY tool
SORT length(rows) DESC
```

### Per-Session Tool Counts

```dataview
TABLE
  date AS "Date",
  project AS "Project",
  tool_uses AS "Tool Uses",
  tools AS "Tools"
FROM "Projects"
WHERE type = "session" AND tool_uses != null AND tool_uses > 0
SORT date DESC, iteration DESC
LIMIT 30
```

## Project Summary

```dataview
TABLE
  length(rows) AS "Sessions",
  max(rows.date) AS "Last Active",
  (sum(rows.tokens_in) + sum(rows.tokens_out)) AS "Total Tokens",
  round(sum(rows.friction_score) / length(rows)) AS "Avg Friction"
FROM "Projects"
WHERE type = "session" AND project != null AND project != ""
GROUP BY project
SORT max(rows.date) DESC
```
