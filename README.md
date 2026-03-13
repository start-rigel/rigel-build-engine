# rigel-build-engine

Build engine for canonical model normalization, daily price aggregation, and minimal hard compatibility checks.

## Language

Go

## Current Stage

Transitioning from early MVP build-generation logic toward a price-catalog-centered engine.

## Intended Role

- read collected JD and Goofish product samples from PostgreSQL
- normalize raw titles into canonical categories, brands, and models
- map products into canonical parts
- aggregate daily prices per canonical model
- provide a clean price catalog for AI consumption
- keep only minimal hard checks that should not be left entirely to AI

## Implemented

- current build-generation endpoints are still available
- `GET /api/v1/catalog/prices` returns an AI-ready aggregated price catalog
- price catalog groups samples by canonical model key instead of raw product title
- historical mock products are excluded from the price catalog
- RAM titles now collapse into generic canonical forms such as `DDR5 6000 32G`
- CPU, GPU, and SSD titles now also collapse into tighter model-level forms such as `Ryzen 7500F`, `RTX 4060`, and `SN770 1TB NVMe`

## Routes

- `GET /healthz`
- `GET /api/v1/catalog/prices`
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

- TODO: persist canonical daily summaries into `part_market_summary`
- TODO: expose a cleaner AI-facing payload contract centered on the price catalog
- TODO: reduce remaining dependence on starter fallback data over time
