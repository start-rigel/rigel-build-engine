# rigel-build-engine

Build engine for accepting UI-originated request parameters, organizing hardware price data, and producing recommendation analysis.

## Language

Go

## Current Stage

Transitioning toward a UI-parameter-driven analysis engine centered on the current hardware price catalog.

## Intended Role

- accept request parameters forwarded from `rigel-console`
- read collected JD product samples from PostgreSQL
- normalize raw titles into canonical categories, brands, and models
- organize current hardware information into a structured price catalog
- request AI API analysis using `budget + use case + organized hardware info`
- return recommendation/explanation output to `rigel-console`
- keep only minimal hard checks that should not be left entirely to AI

## Implemented

- current build-generation endpoints are still available
- `GET /api/v1/catalog/prices` returns an AI-ready aggregated price catalog
- `POST /api/v1/advice/generate` now returns recommendation analysis from inside build-engine
- `POST /api/v1/advice/catalog` now returns catalog-based recommendation drafts from inside build-engine
- price catalog groups samples by canonical model key instead of raw product title
- historical mock products are excluded from the price catalog
- RAM titles now collapse into generic canonical forms such as `DDR5 6000 32G`
- CPU, GPU, and SSD titles now also collapse into tighter model-level forms such as `Ryzen 7500F`, `RTX 4060`, and `SN770 1TB NVMe`
- generating the catalog now also upserts `parts`, `product_part_mapping`, and daily `part_market_summary` rows

## Routes

- `GET /healthz`
- `GET /api/v1/catalog/prices`
- `POST /api/v1/advice/generate`
- `POST /api/v1/advice/catalog`
- `POST /api/v1/builds/generate`
- `GET /api/v1/builds/{id}`
- `GET /api/v1/parts/search`

## Example Requests

```bash
curl "http://localhost:18082/api/v1/catalog/prices?use_case=gaming&build_mode=mixed&limit=500"
```

```bash
curl -X POST http://localhost:18082/api/v1/builds/generate \
  -H 'Content-Type: application/json' \
  -d '{"budget":6000,"use_case":"gaming","build_mode":"new_only"}'
```

## TODO / MOCK

- TODO: replace the current local template advice path with a real external AI API call
- TODO: reduce remaining dependence on starter fallback data over time
