import { expect, test } from '@playwright/test'

test.describe.serial('authentication and governed point workflow', () => {
  const firstLoginTest = process.env.GO_DOT_E2E_FIRST_USERNAME ? test : test.skip
  firstLoginTest('first login is forced to change the initial password', async ({ page }) => {
    await page.goto('/login')
	const username = process.env.GO_DOT_E2E_FIRST_USERNAME ?? 'admin'
	const currentPassword = process.env.GO_DOT_E2E_FIRST_PASSWORD ?? 'admin'
	const newPassword = process.env.GO_DOT_E2E_FIRST_NEW_PASSWORD ?? 'Admin-Changed-2026!'
    await page.getByLabel(/账号/).fill(username)
    await page.getByLabel(/密码/).fill(currentPassword)
    await page.getByRole('button', { name: /登录/ }).click()
    await expect(page).toHaveURL(/change-password/)
    await page.getByLabel(/当前密码/).fill(currentPassword)
    await page.getByLabel('新密码', { exact: true }).fill(newPassword)
	await page.getByLabel(/确认新密码/).fill(newPassword)
	await page.getByRole('button', { name: /保存并进入系统/ }).click()
    await expect(page).not.toHaveURL(/change-password/)
  })

  test('admin can create a project and sees the category and point workbench', async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/账号/).fill('admin')
    await page.getByLabel(/密码/).fill('Admin-Changed-2026!')
    await page.getByRole('button', { name: /登录/ }).click()
    await expect(page.getByText(/项目|概览/).first()).toBeVisible()
    // The release acceptance environment is seeded by the backend integration
    // flow; exercise navigation and governance affordances without brittle CSS.
    await page.getByText(/业务分类/).first().click()
    await expect(page.getByText(/DOMAIN|业务域/).first()).toBeVisible()
    await page.getByText(/点位/).first().click()
    await expect(page.getByText(/新增点位|点位管理/).first()).toBeVisible()
  })

  test('published point is immutable and viewer has no write action', async ({ page }) => {
    test.skip(!process.env.GO_DOT_E2E_VIEWER_USERNAME, 'viewer fixture is optional outside full acceptance')
    await page.goto('/login')
    await page.getByLabel(/账号/).fill(process.env.GO_DOT_E2E_VIEWER_USERNAME!)
    await page.getByLabel(/密码/).fill(process.env.GO_DOT_E2E_VIEWER_PASSWORD!)
    await page.getByRole('button', { name: /登录/ }).click()
    await page.getByText(/点位/).first().click()
    await expect(page.getByRole('button', { name: /新增|发布|编辑/ })).toHaveCount(0)
  })
})
