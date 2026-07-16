import { expect, test, type APIRequestContext } from '@playwright/test'

let projectId = 0
let pointId = 0
let categoryId = 0

async function loginAPI(request: APIRequestContext, username: string, password: string) {
  const response = await request.post('/api/v1/auth/login', { data: { username, password } })
  expect(response.ok(), await response.text()).toBeTruthy()
  return response.json() as Promise<{ access_token: string; user: { id: number } }>
}

function auth(token: string) {
  return { Authorization: `Bearer ${token}` }
}

test.describe.serial('authentication and governed point workflow', () => {
  test('first login is forced to change the initial password', async ({ page }) => {
    const currentPassword = process.env.GO_DOT_E2E_FIRST_PASSWORD ?? 'Admin-Changed-2026!'
    const newPassword = process.env.GO_DOT_E2E_FIRST_NEW_PASSWORD ?? 'First-Changed-2026!'
    await page.goto('/login')
    await page.getByLabel(/账号/).fill(process.env.GO_DOT_E2E_FIRST_USERNAME ?? 'e2e_first')
    await page.getByLabel(/密码/).fill(currentPassword)
    await page.getByRole('button', { name: /登录/ }).click()
    await expect(page).toHaveURL(/change-password/)
    await page.getByLabel(/当前密码/).fill(currentPassword)
    await page.getByLabel('新密码', { exact: true }).fill(newPassword)
    await page.getByLabel(/确认新密码/).fill(newPassword)
    await page.getByRole('button', { name: /保存并进入系统/ }).click()
    await expect(page).not.toHaveURL(/change-password/)
  })

  test('admin creates three category levels, publishes a unique point, and publication is immutable', async ({ page, request }) => {
    test.setTimeout(90_000)
    const password = process.env.GO_DOT_E2E_ADMIN_PASSWORD ?? 'Admin-Changed-2026!'
    const adminUsername = process.env.GO_DOT_E2E_WORKFLOW_ADMIN_USERNAME ?? 'e2e_admin'
    const suffix = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`
    await page.goto('/login')
    await page.getByLabel(/账号/).fill(adminUsername)
    await page.getByLabel(/密码/).fill(password)
    await page.getByRole('button', { name: /登录/ }).click()
    await expect(page).toHaveURL(/onboarding/)

    const projectName = `E2E Project ${suffix}`
    const domainName = `Domain ${suffix}`
    const moduleName = `Module ${suffix}`
    const sceneName = `Scene ${suffix}`
    const pointName = `Point ${suffix}`
    await page.getByLabel('项目名称').fill(projectName)
    await page.getByLabel('项目编码').fill(`e2e_${suffix}`)
    await page.getByRole('button', { name: '创建项目' }).click()
    await expect(page.getByRole('heading', { name: '项目创建成功' })).toBeVisible()
    await page.getByRole('button', { name: /进入项目/ }).click()
    await expect(page).toHaveURL(/\/projects\/(\d+)\/overview/)
    projectId = Number(page.url().match(/\/projects\/(\d+)\//)?.[1])
    expect(projectId).toBeGreaterThan(0)

    await page.getByRole('link', { name: /业务分类/ }).click()
    for (const item of [
      { level: 'DOMAIN', code: `domain_${suffix}`, name: domainName },
      { level: 'MODULE', code: `module_${suffix}`, name: moduleName, parent: domainName },
      { level: 'SCENE', code: `scene_${suffix}`, name: sceneName, parent: moduleName },
    ]) {
      await page.getByRole('button', { name: '新增分类' }).click()
      const dialog = page.getByRole('dialog')
      await dialog.getByLabel('层级').selectOption(item.level)
      if (item.parent) await dialog.getByLabel('父分类').selectOption({ label: item.parent })
      await dialog.getByLabel('编码').fill(item.code)
      await dialog.getByLabel('名称').fill(item.name)
      await dialog.getByRole('button', { name: '保存' }).click()
      await expect(dialog).not.toBeVisible()
      await expect(page.getByText(item.name, { exact: false }).first()).toBeVisible()
    }

    await page.getByRole('link', { name: /点位管理/ }).click()
    await page.getByRole('button', { name: '新增点位' }).click()
    let wizard = page.getByRole('dialog', { name: '点位编辑向导' })
    await wizard.getByLabel('业务场景').selectOption({ label: `${sceneName}（scene_${suffix}）` })
    await wizard.getByLabel('点位编码').fill(`product_detail_view_${suffix}`)
    await wizard.getByLabel('点位名称').fill(pointName)
    await wizard.getByLabel('行为类型').selectOption('PAGE_VIEW')
    await wizard.getByRole('button', { name: '触发与统计' }).click()
    await wizard.getByLabel('触发时机').fill('详情主体加载完成后一次')
    await wizard.getByLabel('触发次数规则').fill('页面生命周期一次')
    await wizard.getByLabel('UV 模式').selectOption('EXACT')
    await wizard.getByLabel('统计时间').selectOption('SERVER')
    await wizard.getByRole('button', { name: '保存草稿' }).click()
    await expect(wizard).not.toBeVisible()

    const pointRow = page.getByRole('row').filter({ hasText: pointName })
    await pointRow.getByRole('button', { name: '编辑' }).click()
    wizard = page.getByRole('dialog', { name: '点位编辑向导' })
    await wizard.getByRole('button', { name: /协作与发布/ }).click()
    await wizard.getByLabel('产品负责人').fill('E2E Product')
    await wizard.getByLabel('开发负责人').fill('E2E Developer')
    await wizard.getByRole('button', { name: '保存草稿' }).click()
    await expect(wizard).not.toBeVisible()
    await pointRow.getByRole('button', { name: '编辑' }).click()
    wizard = page.getByRole('dialog', { name: '点位编辑向导' })
    await wizard.getByRole('button', { name: /协作与发布/ }).click()
    await wizard.getByRole('button', { name: '发布点位' }).click()
    await expect(page.getByText('PUBLISHED', { exact: true })).toBeVisible()
    await expect(page.locator('[data-test="edit-point"]')).toHaveCount(0)

    const session = await loginAPI(request, adminUsername, password)
    const headers = auth(session.access_token)
    const categories = await (await request.get(`/api/v1/projects/${projectId}/categories`, { headers })).json()
    categoryId = categories.find((item: { name: string }) => item.name === sceneName).id
    const points = await (await request.get(`/api/v1/projects/${projectId}/points`, { headers })).json()
    pointId = points.items.find((item: { name: string }) => item.name === pointName).id
    const immutable = await request.patch(`/api/v1/projects/${projectId}/points/${pointId}`, {
      headers,
      data: { category_id: categoryId, code: `mutated_${suffix}`, name: 'mutated' },
    })
    expect(immutable.status(), await immutable.text()).toBe(409)
    const addViewer = await request.post(`/api/v1/projects/${projectId}/members`, {
      headers,
      data: { username: process.env.GO_DOT_E2E_VIEWER_USERNAME ?? 'e2e_viewer', role: 'VIEWER' },
    })
    expect(addViewer.status(), await addViewer.text()).toBe(201)
  })

  test('viewer UI is read-only and direct management writes return 403', async ({ page, request }) => {
    expect(projectId).toBeGreaterThan(0)
    const username = process.env.GO_DOT_E2E_VIEWER_USERNAME ?? 'e2e_viewer'
    const password = process.env.GO_DOT_E2E_VIEWER_PASSWORD ?? 'Admin-Changed-2026!'
    const session = await loginAPI(request, username, password)
    const headers = auth(session.access_token)
    const writes: Array<[string, string, unknown?]> = [
      ['patch', `/api/v1/projects/${projectId}/settings`, { timezone: 'UTC', allowed_origins: [] }],
      ['post', `/api/v1/projects/${projectId}/rotate-write-key`, {}],
      ['post', `/api/v1/projects/${projectId}/members`, { username, role: 'VIEWER' }],
      ['delete', `/api/v1/projects/${projectId}/members/${session.user.id}`],
      ['post', `/api/v1/projects/${projectId}/categories`, { code: 'forbidden', name: 'Forbidden', level: 'DOMAIN', sort_order: 1 }],
      ['patch', `/api/v1/projects/${projectId}/categories/${categoryId}`, { name: 'Forbidden', sort_order: 1, active: true }],
      ['post', `/api/v1/projects/${projectId}/points`, { category_id: categoryId, code: 'forbidden_point', name: 'Forbidden' }],
      ['patch', `/api/v1/projects/${projectId}/points/${pointId}`, { category_id: categoryId, code: 'forbidden_point', name: 'Forbidden' }],
      ['delete', `/api/v1/projects/${projectId}/points/${pointId}`],
      ['post', `/api/v1/projects/${projectId}/points/${pointId}/publish`, {}],
      ['post', `/api/v1/projects/${projectId}/points/${pointId}/copy`, { code: 'forbidden_copy' }],
      ['post', `/api/v1/projects/${projectId}/points/${pointId}/deprecate`, { reason: 'forbidden' }],
      ['patch', `/api/v1/projects/${projectId}/points/${pointId}/development-state`, { state: 'ONLINE' }],
    ]
    for (const [method, url, data] of writes) {
      const response = await request.fetch(url, { method, headers, data })
      expect(response.status(), `${method.toUpperCase()} ${url}: ${await response.text()}`).toBe(403)
    }

    await page.goto('/login')
    await page.getByLabel(/账号/).fill(username)
    await page.getByLabel(/密码/).fill(password)
    await page.getByRole('button', { name: /登录/ }).click()
    await page.goto(`/projects/${projectId}/points/${pointId}`)
    await expect(page.getByRole('button', { name: /新增|发布|编辑|废弃/ })).toHaveCount(0)
  })
})
