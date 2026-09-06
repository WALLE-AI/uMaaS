# uMaaS Product API Design

This directory defines the product control-plane API consumed by the React application. It is separate from the OpenAI-compatible inference data plane.

| Plane | Base URL | Purpose |
| --- | --- | --- |
| Product control plane | `/api/v1` | Catalog, analytics, docs, harness configuration, identity |
| Model inference data plane | `https://api.umaas.dev/v1` | Chat completions and future embeddings, images, audio, and responses APIs |

The backend-facing HTTP contract now lives at `frontend/contracts/openapi.yaml` — it is shared by both the web portal and the admin console, so it is no longer owned by `web/`. `contracts.ts` and `services.ts` here remain the web-facing types and endpoint functions.

## Route Component Analysis

| Frontend route | Current page data | Required API |
| --- | --- | --- |
| `/` | Model/provider counts, monthly tokens, gateway latency, three featured models | `GET /catalog/summary` |
| `/models` | Search, provider, modality, capability, context, price, ZDR filters; release/price/speed sorting; counts and cursor pagination | `GET /models`, `GET /me/favorites`, `PUT/DELETE /me/favorites/{model_id}` |
| `/models/:provider/:model` | Model metadata, endpoints, pricing, latency, throughput, uptime, benchmark scores, apps, activity, FAQ and related models | `GET /models/{provider}/{model}` |
| `/benchmarks` | Category tabs, run totals, quality/value/speed winners and benchmark result drawer | `GET /benchmarks`, `GET /benchmarks/{slug}` |
| `/rankings` | Modality, time period, unit and open-source scope across 13 ranking dimensions | `GET /rankings` |
| `/docs/*` | Navigation tree, article content, table of contents and search | `GET /docs/navigation`, `GET /docs/{slug}`, `GET /docs/search` |
| `/harness` | Supported harnesses, generated install command/config, waitlist action | `GET /harnesses`, `POST /harnesses/config`, `POST /harnesses/waitlist` |
| `/login`, `/signup` | Email session, registration, OAuth initiation, password reset | `/auth/*`, `GET /me` |

The model compare selection, ranking chart scale, open drawer, active navigation item, code language tab, and unsaved Harness form are UI state and should remain client-side.

## Request Conventions

- Public catalog, benchmark, ranking, and docs reads do not require authentication.
- User-specific favorites, waitlists, and identity endpoints require a secure session cookie.
- Browser sessions use `HttpOnly`, `Secure`, and `SameSite=Lax` cookies. Access tokens are not stored in `localStorage`.
- List endpoints use cursor pagination: `cursor`, `limit`, and `meta.next_cursor`.
- Repeated query keys represent arrays, for example `?modality=text&modality=image`.
- Canonical model IDs use `provider/model`; HTTP resource paths expose these as two path parameters to avoid encoded-slash ambiguity.
- Time values use ISO 8601 UTC. Money values are numeric USD amounts, never display strings.
- Analytics are aggregated by complete UTC periods and expose `updated_at` so the UI can label freshness.
- `request_id` is returned on success and error for support and tracing.

Successful response:

```json
{
  "data": [{ "id": "google/gemini-3.8-flash", "name": "Gemini 3.8 Flash" }],
  "meta": { "next_cursor": "eyJvZmZzZXQiOjIwfQ", "total": 431 },
  "request_id": "req_01K4..."
}
```

Error response:

```json
{
  "error": {
    "code": "validation_failed",
    "message": "limit must be between 1 and 100",
    "field": "limit"
  },
  "request_id": "req_01K4..."
}
```

## Status and Error Semantics

| Status | Meaning |
| --- | --- |
| `400` | Malformed request or unsupported filter combination |
| `401` | No valid session |
| `403` | Session exists but lacks workspace permission |
| `404` | Resource does not exist or is not visible to the caller |
| `409` | Resource state conflict, such as an existing account |
| `422` | Field validation failed |
| `429` | Rate limited; respect `Retry-After` |
| `5xx` | Server/provider failure; safe reads may be retried with jitter |

## Cache Policy

| Resource | Suggested browser/CDN policy |
| --- | --- |
| Catalog summary and model list | `public, max-age=60, stale-while-revalidate=300` |
| Model detail and providers | `public, max-age=30, stale-while-revalidate=120` |
| Benchmarks | `public, max-age=300, stale-while-revalidate=3600` |
| Rankings | `public, max-age=300, stale-while-revalidate=900` |
| Docs | `public, max-age=300`, with `ETag` |
| Session, favorites and generated config | `private, no-store` |

## Integration Order

1. Replace `src/data.ts` model reads with `catalogApi` and `modelsApi`.
2. Connect benchmark and ranking pages; keep their view-only controls local.
3. Move docs content to the docs API or a versioned static content build.
4. Connect Harness config generation and authenticated waitlist submission.
5. Replace the login prototype with cookie-based auth and server OAuth initiation.

Until a backend exists, the current in-memory data remains the UI fallback. Do not silently mix live and mock records in the same list; expose an explicit development adapter if mocks are still needed during integration.
