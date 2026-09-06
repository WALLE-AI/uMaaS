# Component Architecture

The component tree is organized by ownership and reuse scope.

```text
components/
|-- analytics/ Data visualization, ranking, and benchmark components
|-- common/   Product-wide primitives with no feature-specific state
|-- console/  Authenticated workspace navigation and console primitives
|-- layout/   Application shell, global navigation, and footer composition
|-- models/   Components owned by the model catalog domain
`-- index.ts  Public component API
```

## Conventions

- Import shared components from `src/components`, not another folder's internal file.
- Keep route state, data loading, filtering, and submission logic in page components.
- Keep components focused on rendering and local interaction state.
- Put navigation, theme tokens, and other application-wide constants in `src/config`.
- Add a component to its local `index.ts`, then expose it from the root barrel only when pages need it.
- Reuse the existing Ant Design and CSS classes before introducing a new visual primitive.

## Placement Examples

- A reusable empty state belongs in `common`.
- A ranking chart, benchmark table, or analytics summary belongs in `analytics`.
- A desktop/mobile navigation change belongs in `layout`.
- A provider badge or model comparison row belongs in `models`.
- A workspace sidebar, account summary, or console page header belongs in `console`.
- Benchmark-only tables should live in a future `components/benchmarks` domain folder.
