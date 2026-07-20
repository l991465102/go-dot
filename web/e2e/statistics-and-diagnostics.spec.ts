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

test('admin queries, saves, and downloads a two-point custom report', async ({ page }) => {
  await page.getByText('自定义报表', { exact: true }).first().click()
  await expect(page.getByRole('heading', { name: '自定义报表' })).toBeVisible()
  await expect(page.getByText('统计时区：Asia/Shanghai').first()).toBeVisible()

  const pointSelect = page.getByLabel('点位选择')
  await pointSelect.click()
  await page.getByRole('option', { name: /商品详情浏览.*product_detail_view/ }).click()
  await page.getByRole('option', { name: /结算提交.*checkout_submit/ }).click()
  await page.keyboard.press('Escape')

  await page.locator('[data-test="report-name"] input').fill(`Playwright 验收 ${Date.now()}`)
  await page.locator('[data-test="query-report"]').click()
  await expect(page.getByText('统计时区：Asia/Shanghai').last()).toBeVisible()
  await expect(page.getByRole('columnheader', { name: 'PV' })).toBeVisible()
  await expect(page.getByRole('columnheader', { name: 'UV' })).toBeVisible()
  await expect(page.getByText('EXACT').first()).toBeVisible()
  await expect(page.getByText('APPROXIMATE').first()).toBeVisible()

  await page.locator('[data-test="save-as-report"]').click()
  await expect(page.getByText('报表已保存')).toBeVisible()

  const download = page.waitForEvent('download')
  await page.locator('[data-test="export-report"]').click()
  const file = await download
  expect(file.suggestedFilename()).toMatch(/\.xlsx$/)
})

test('diagnostics displays invalid event details without a replay action', async ({ page }) => {
  await page.getByText(/数据诊断|诊断/).first().click()
  await expect(page.getByText(/异常事件|无效事件/).first()).toBeVisible()
  await expect(page.getByRole('button', { name: /重放|重试/ })).toHaveCount(0)

  await page.getByRole('tab', { name: '最近事件' }).click()
  const timestamp = page.locator('.el-table__body tbody tr').first().locator('td').first()
  await expect(timestamp).toHaveText(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/)
  const displayed = (await timestamp.textContent()) ?? ''
  expect(displayed).not.toContain('T')
  expect(displayed).not.toMatch(/Z\s*$/)
})
