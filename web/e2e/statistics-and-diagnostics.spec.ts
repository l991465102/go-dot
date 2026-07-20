import { expect, test, type APIRequestContext } from '@playwright/test'

let projectId = 0
let exactPointName = ''
let approximatePointName = ''

function auth(token: string) {
  return { Authorization: `Bearer ${token}` }
}

async function seedStatisticsProject(request: APIRequestContext) {
  const username = process.env.GO_DOT_E2E_WORKFLOW_ADMIN_USERNAME ?? 'e2e_admin'
  const password = process.env.GO_DOT_E2E_ADMIN_PASSWORD ?? 'Admin-Changed-2026!'
  const login = await request.post('/api/v1/auth/login', { data: { username, password } })
  expect(login.ok(), await login.text()).toBeTruthy()
  const token = (await login.json() as { access_token: string }).access_token
  const headers = auth(token)
  const suffix = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`
  const projectResponse = await request.post('/api/v1/projects', {
    headers,
    data: { code: `reports_${suffix}`, name: `Reports ${suffix}`, timezone: 'Asia/Shanghai', allowed_origins: [] },
  })
  expect(projectResponse.status(), await projectResponse.text()).toBe(201)
  const creation = await projectResponse.json() as { project: { id: number }; write_key: string }
  projectId = creation.project.id

  let parentId: number | undefined
  for (const [level, code] of [['DOMAIN', 'domain'], ['MODULE', 'module'], ['SCENE', 'scene']] as const) {
    const category = await request.post(`/api/v1/projects/${projectId}/categories`, {
      headers,
      data: { code: `${code}_${suffix}`, name: `${level} ${suffix}`, level, sort_order: 1, ...(parentId ? { parent_id: parentId } : {}) },
    })
    expect(category.status(), await category.text()).toBe(201)
    parentId = (await category.json() as { id: number }).id
  }

  exactPointName = `报表精确点位 ${suffix}`
  approximatePointName = `报表近似点位 ${suffix}`
  for (const point of [
    { code: `report_exact_${suffix}`, name: exactPointName, uv_mode: 'EXACT' },
    { code: `report_approx_${suffix}`, name: approximatePointName, uv_mode: 'APPROXIMATE' },
  ]) {
    const created = await request.post(`/api/v1/projects/${projectId}/points`, {
      headers,
      data: {
        category_id: parentId, code: point.code, name: point.name, behavior_type: 'PAGE_VIEW', development_state: 'ONLINE',
        trigger_timing: 'Playwright fixture', trigger_count_rule: '每次一次', count_pv: true, count_uv: true,
        uv_mode: point.uv_mode, time_basis: 'SERVER', supported_platforms: ['web'], supported_channels: [],
        product_owner: 'playwright', properties: [],
      },
    })
    expect(created.status(), await created.text()).toBe(201)
    const pointId = (await created.json() as { id: number }).id
    const published = await request.post(`/api/v1/projects/${projectId}/points/${pointId}/publish`, { headers, data: {} })
    expect(published.ok(), await published.text()).toBeTruthy()
  }

  const collected = await request.post('/api/v1/collect/events', {
    headers: { 'X-Write-Key': creation.write_key },
    data: { events: [{ event_id: '018f22a0-2c5a-7bb0-8c1a-4f15b2ae0001', point_code: `report_exact_${suffix}`, event_time: Date.now(), anonymous_id: `diagnostic-${suffix}`, platform: 'web', properties: {} }] },
  })
  expect(collected.status(), await collected.text()).toBe(202)
}

test.beforeAll(async ({ request }) => {
  await seedStatisticsProject(request)
})

test.beforeEach(async ({ page }) => {
  await page.goto('/login')
  await page.getByLabel(/账号/).fill(process.env.GO_DOT_E2E_WORKFLOW_ADMIN_USERNAME ?? 'e2e_admin')
  await page.getByLabel(/密码/).fill(process.env.GO_DOT_E2E_ADMIN_PASSWORD ?? 'Admin-Changed-2026!')
  await page.getByRole('button', { name: /登录/ }).click()
  await page.waitForURL(/\/projects\/\d+\/overview/)
  await page.goto(`/projects/${projectId}/overview`)
  await expect(page.getByRole('link', { name: '自定义报表' })).toBeVisible()
})

test('overview exposes explicit latency and data-quality labels', async ({ page }) => {
  await expect(page.getByText(/延迟|实时|聚合/).first()).toBeVisible()
  await expect(page.getByText(/无上报|异常率|死信/).first()).toBeVisible()
})

test('statistics supports dimensions and exact or approximate UV labels', async ({ page }) => {
  await page.getByRole('link', { name: '统计分析' }).click()
  await expect(page.getByLabel('范围')).toBeVisible()
  await expect(page.getByLabel('粒度')).toBeVisible()
  await expect(page.getByLabel('点位编码')).toBeVisible()
  await expect(page.getByLabel('平台')).toBeVisible()
  await expect(page.getByLabel('渠道')).toBeVisible()
  await expect(page.getByText(/精确|近似/).first()).toBeVisible()
})

test('admin queries, saves, and downloads a two-point custom report', async ({ page }) => {
  test.setTimeout(90_000)
  await page.getByRole('link', { name: '自定义报表' }).click()
  await expect(page.getByRole('heading', { name: '自定义报表' })).toBeVisible()
  await expect(page.getByText('统计时区：Asia/Shanghai').first()).toBeVisible()

  const pointSelect = page.locator('[data-test="point-select"]')
  await pointSelect.click()
  await page.getByRole('option', { name: new RegExp(exactPointName) }).click()
  await page.getByRole('option', { name: new RegExp(approximatePointName) }).click()
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
  await page.getByRole('link', { name: '数据诊断' }).click()
  await expect(page.getByText(/异常事件|无效事件/).first()).toBeVisible()
  await expect(page.getByRole('button', { name: /重放|重试/ })).toHaveCount(0)

  await page.getByRole('tab', { name: '最近事件' }).click()
  const timestamp = page.locator('.el-table__body tbody tr').first().locator('td').first()
  await expect(timestamp).toHaveText(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/)
  const displayed = (await timestamp.textContent()) ?? ''
  expect(displayed).not.toContain('T')
  expect(displayed).not.toMatch(/Z\s*$/)
})
