import { expect, test } from '@playwright/test'

test.beforeEach(async ({ page }) => {
  await page.goto('/login')
  await page.getByLabel(/账号/).fill('admin')
  await page.getByLabel(/密码/).fill(process.env.GO_DOT_E2E_ADMIN_PASSWORD ?? 'Admin-Changed-2026!')
  await page.getByRole('button', { name: /登录/ }).click()
})

test('overview exposes explicit latency and data-quality labels', async ({ page }) => {
  await expect(page.getByText(/延迟|实时|聚合/).first()).toBeVisible()
  await expect(page.getByText(/无上报|异常率|死信/).first()).toBeVisible()
})

test('statistics supports dimensions and exact or approximate UV labels', async ({ page }) => {
  await page.getByText(/统计分析/).first().click()
  await expect(page.getByLabel('范围')).toBeVisible()
  await expect(page.getByLabel('粒度')).toBeVisible()
  await expect(page.getByLabel('点位编码')).toBeVisible()
  await expect(page.getByLabel('平台')).toBeVisible()
  await expect(page.getByLabel('渠道')).toBeVisible()
  await expect(page.getByText(/精确|近似/).first()).toBeVisible()
})

test('diagnostics displays invalid event details without a replay action', async ({ page }) => {
  await page.getByText(/数据诊断|诊断/).first().click()
  await expect(page.getByText(/异常事件|无效事件/).first()).toBeVisible()
  await expect(page.getByRole('button', { name: /重放|重试/ })).toHaveCount(0)
})
