# Query Array Serialization Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every Vue API request encode array query parameters as repeated plain keys so Go handlers receive `point_code` instead of `point_code[]`.

**Architecture:** Configure the shared Axios instance once with Axios's `indexes: null` query serializer. Exercise the real Axios URI builder in a regression test so statistics, overview, and diagnostics inherit the same canonical behavior without module-specific URL construction.

**Tech Stack:** Vue 3.5, TypeScript 6, Axios 1.18, Vitest 4, Vite 8

## Global Constraints

- Canonical single value: `point_code=android_uv`.
- Canonical multiple values: `point_code=a&point_code=b`.
- Generated URLs must not contain `point_code[]` or `%5B%5D`.
- Backend query parsing and API contracts remain unchanged.
- Existing authentication refresh/replay behavior must remain green.

---

### Task 1: Configure and verify canonical Axios array serialization

**Files:**
- Modify: `web/src/api/client.spec.ts`
- Modify: `web/src/api/client.ts`

**Interfaces:**
- Consumes: `apiClient`, the shared Axios instance used by statistics and diagnostics clients.
- Produces: Axios query serialization where array values use repeated unbracketed keys.

- [ ] **Step 1: Write the failing URI regression tests**

Append a new describe block to `web/src/api/client.spec.ts` that exercises Axios itself rather than a mock serializer:

```ts
describe('API query serialization', () => {
  it('serializes a single array value without square brackets', () => {
    const uri = apiClient.getUri({
      url: '/statistics/realtime',
      params: { point_code: ['android_uv'] },
    })

    expect(uri).toContain('point_code=android_uv')
    expect(uri).not.toContain('point_code%5B%5D')
    expect(uri).not.toContain('point_code[]')
  })

  it('serializes multiple array values as repeated plain keys', () => {
    const uri = apiClient.getUri({
      url: '/diagnostics/recent-events',
      params: { point_code: ['android_uv', 'web_view'] },
    })

    const query = uri.split('?')[1] ?? ''
    const values = new URLSearchParams(query).getAll('point_code')
    expect(values).toEqual(['android_uv', 'web_view'])
  })
})
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
npm --prefix web test -- --run src/api/client.spec.ts
```

Expected: FAIL because Axios currently generates `point_code%5B%5D=android_uv` and `URLSearchParams.getAll('point_code')` returns an empty array.

- [ ] **Step 3: Configure the shared Axios serializer**

Change the `axios.create` call in `web/src/api/client.ts` to:

```ts
export const apiClient = axios.create({
  baseURL: '/api/v1',
  withCredentials: true,
  paramsSerializer: { indexes: null },
})
```

Do not add per-endpoint serialization or change backend parsing.

- [ ] **Step 4: Run the focused test and verify GREEN**

Run:

```bash
npm --prefix web test -- --run src/api/client.spec.ts
```

Expected: all client authentication and query serialization tests PASS.

- [ ] **Step 5: Run the frontend regression gates**

Run:

```bash
npm --prefix web test -- --run
npm --prefix web run typecheck
npm --prefix web run build
git diff --check
```

Expected: 0 failed tests, typecheck/build exit 0, and no whitespace errors. The existing Vite chunk-size advisory is non-blocking.

- [ ] **Step 6: Rebuild and verify the running overview request**

Rebuild the isolated web service and open the project overview. In the browser network request, verify the realtime URL contains `point_code=android_uv`, contains no encoded brackets, and returns HTTP 200.

- [ ] **Step 7: Commit the fix**

```bash
git add web/src/api/client.ts web/src/api/client.spec.ts
git commit -m "fix: serialize array query parameters"
```
