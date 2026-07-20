# Emergency Write Key Rotation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the permanent write-key lifecycle explicit and prevent accidental emergency rotations while preserving the existing 24-hour migration window.

**Architecture:** Keep the backend contract and lifecycle unchanged. Add a local confirmation dialog to the project settings component so the first click is reversible and only an explicit confirmation calls the existing rotation endpoint.

**Tech Stack:** Vue 3, TypeScript, Element Plus, Vitest, Vue Test Utils.

---

### Task 1: Clarify and confirm emergency write-key rotation

**Files:**
- Modify: `web/src/views/ProjectSettingsView.vue`
- Modify: `web/src/views/ProjectSettingsView.spec.ts`

**Interfaces:**
- Preserve: `rotateWriteKey(projectId)` API and its one-time `write_key` response.
- Add UI state: a local dangerous confirmation dialog; opening or cancelling it must not call the API.

- [ ] **Step 1: Write failing component tests**

Add assertions that:

- Admin and Viewer both see `当前写入密钥永久有效，不会自动轮换。仅在确认密钥泄露时执行紧急轮换。`.
- Only Admin sees the exact button text `紧急轮换写入密钥`.
- Clicking the button opens the confirmation dialog and does not call `rotateWriteKey`.
- Cancelling closes the dialog without calling the API.
- Explicit confirmation calls `rotateWriteKey` exactly once.
- A successful response shows the one-time key and the old-key expiry formatted in `Asia/Shanghai`.
- A failed response shows the existing error state and no plaintext key.

- [ ] **Step 2: Run the focused test and confirm RED**

Run:

```bash
cd web
npm test -- --run src/views/ProjectSettingsView.spec.ts
```

Expected: FAIL because the permanent-key explanation, renamed action, and confirmation dialog are not implemented.

- [ ] **Step 3: Implement the local confirmation flow**

In `ProjectSettingsView.vue`:

- Add a boolean `rotationConfirmOpen` state.
- Rename the action to `紧急轮换写入密钥` and make its click only open the dialog.
- Add a warning dialog whose body is exactly:
  `轮换后所有上报端都需要切换到新密钥。旧密钥仅保留 24 小时，请确认已准备好协调各端更新。`
- Make cancel the default/reversible action.
- Let the explicit confirm action close the dialog and call the existing `rotate()` once.
- Keep the existing project-switch stale-response protection, one-time plaintext display, error handling, and Shanghai expiry formatter.
- Add the permanent-key lifecycle explanation for both roles.

- [ ] **Step 4: Run focused checks**

Run:

```bash
cd web
npm test -- --run src/views/ProjectSettingsView.spec.ts
npm run typecheck
```

Expected: tests PASS and typecheck exits 0.

- [ ] **Step 5: Commit**

```bash
git add web/src/views/ProjectSettingsView.vue web/src/views/ProjectSettingsView.spec.ts
git commit -m "feat: clarify emergency write key rotation"
```
