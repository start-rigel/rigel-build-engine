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

## Current Notes

- The service still contains early MVP build-generation code.
- The direction is now shifting toward `price aggregation first, recommendation second`.
- Heavy rule-engine behavior is no longer the primary goal.
- Minimal checks such as CPU/mainboard platform and mainboard/RAM type are still valid.

## Routes

- `GET /healthz`
- `POST /api/v1/builds/generate`
- `GET /api/v1/builds/{id}`
- `GET /api/v1/parts/search`

## TODO / MOCK

- TODO: expose an AI-ready canonical price catalog endpoint
- TODO: make `part_market_summary` the main output artifact
- TODO: reduce remaining dependence on starter fallback data over time
