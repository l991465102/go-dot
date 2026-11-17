import { expect, test } from '@playwright/test'
import { randomUUID } from 'node:crypto'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { Client as WebSDK } from '../../sdk/web/index.js'

test('2.0: requirement, test isolation, acceptance and production funnel', async ({ page, request }) => {
  test.skip(process.env.GO_DOT_E2E_V2 !== '1', 'requires isolated v2 acceptance stack')
  test.setTimeout(120_000)
  const password=process.env.GO_DOT_E2E_ADMIN_PASSWORD || 'Admin-Changed-2026!'
  const login=await request.post('/api/v1/auth/login',{data:{username:'admin',password}});expect(login.status()).toBe(200)
  const session=await login.json() as {access_token:string;user:{id:number}};const headers={Authorization:`Bearer ${session.access_token}`};const suffix=randomUUID().replaceAll('-','').slice(0,12)
  const created=await request.post('/api/v1/projects',{headers,data:{code:`v2_${suffix}`,name:`V2 ${suffix}`,timezone:'Asia/Shanghai',allowed_origins:[]}});expect(created.status(),await created.text()).toBe(201)
  const {project,write_key:writeKey}=await created.json() as {project:{id:number};write_key:string};const projectId=project.id
  let parent:number|undefined
  for(const level of ['DOMAIN','MODULE','SCENE']){const res=await request.post(`/api/v1/projects/${projectId}/categories`,{headers,data:{code:`${level.toLowerCase()}_${suffix}`,name:level,level,sort_order:0,...(parent?{parent_id:parent}:{})}});expect(res.status(),await res.text()).toBe(201);parent=(await res.json()).id}
  const points:Array<{id:number;code:string}>=[]
  for(const kind of ['view','order']){const code=`${kind}_${suffix}`;const res=await request.post(`/api/v1/projects/${projectId}/points`,{headers,data:{category_id:parent,code,name:kind,behavior_type:'BUSINESS_ACTION',development_state:'PENDING',count_pv:true,count_uv:true,uv_mode:'EXACT',time_basis:'CLIENT',supported_platforms:['web'],supported_channels:[],properties:[]}});expect(res.status(),await res.text()).toBe(201);const p=await res.json() as {id:number};const pub=await request.post(`/api/v1/projects/${projectId}/points/${p.id}/publish`,{headers,data:{}});expect(pub.status(),await pub.text()).toBe(200);points.push({id:p.id,code})}
  const sdkBase=process.env.GO_DOT_E2E_BASE_URL || 'http://127.0.0.1:15173'
  const generated=await promisify(execFile)('node',['sdk/generate.mjs',sdkBase,String(projectId),'ts'],{cwd:process.cwd().endsWith('/web') ? '..' : '.',env:{...process.env,GO_DOT_ACCESS_TOKEN:session.access_token}})
  expect(generated.stdout).toContain(points[0]!.code);expect(generated.stdout).toContain(points[1]!.code)
  const requirement=await request.post(`/api/v1/projects/${projectId}/requirements`,{headers,data:{title:'订单转化需求',goal:'观察下单',scenario:'详情页',requirement_url:'',due_date:''}});expect(requirement.status(),await requirement.text()).toBe(201);const rid=(await requirement.json()).id as number
  const member=await request.post(`/api/v1/projects/${projectId}/members`,{headers,data:{username:'e2e_viewer',role:'VIEWER'}});expect(member.status(),await member.text()).toBe(201)
  const viewerLogin=await request.post('/api/v1/auth/login',{data:{username:'e2e_viewer',password}});expect(viewerLogin.status()).toBe(200);const viewerHeaders={Authorization:`Bearer ${(await viewerLogin.json()).access_token}`}
  expect((await request.get(`/api/v1/projects/${projectId}/requirements`,{headers:viewerHeaders})).status()).toBe(200)
  expect((await request.post(`/api/v1/projects/${projectId}/requirements`,{headers:viewerHeaders,data:{title:'无权限',goal:'',scenario:'',requirement_url:'',due_date:''}})).status()).toBe(403)
  expect((await request.post(`/api/v1/projects/${projectId}/requirements/${rid}/tasks`,{headers:viewerHeaders,data:{point_id:points[0]!.id,platform:'web',assignee_id:null,state:'PENDING'}})).status()).toBe(403)
  const task=await request.post(`/api/v1/projects/${projectId}/requirements/${rid}/tasks`,{headers,data:{point_id:points[0]!.id,platform:'web',assignee_id:session.user.id,state:'TESTING'}});expect(task.status(),await task.text()).toBe(204)
  const tasks=await (await request.get(`/api/v1/projects/${projectId}/requirements/${rid}/tasks`,{headers})).json() as Array<{id:number}>
  const acceptance=await request.post(`/api/v1/projects/${projectId}/requirements/${rid}/tasks/${tasks[0]!.id}/acceptances`,{headers,data:{result:'PASS',note:'验收通过'}});expect(acceptance.status(),await acceptance.text()).toBe(204)
  const baseURL=process.env.GO_DOT_E2E_BASE_URL || 'http://127.0.0.1:15173'
  const webClient=new WebSDK({baseURL,writeKey,test:true,maxRetries:0});webClient.track({point_code:points[0]!.code,anonymous_id:'web-sdk-live',platform:'web',properties:{}});const webResults=await webClient.flush();expect(webResults[0]?.status).toBe('accepted')
  await promisify(execFile)('go',['test','./sdk/go','-run','TestLiveTestEnvironment','-count=1'],{cwd:process.cwd().endsWith('/web') ? '..' : '.',timeout:60000,env:{...process.env,GO_DOT_SDK_TEST_URL:baseURL,GO_DOT_SDK_TEST_KEY:writeKey,GO_DOT_SDK_TEST_POINT:points[0]!.code}})
  const now=Date.now();const event=(code:string,id:string,visitor:string,time:number)=>({event_id:id,point_code:code,event_time:time,anonymous_id:visitor,platform:'web',properties:{}})
  const production=[event(points[0]!.code,'0190b9a8-7000-7000-8000-000000000001','prod-user',now-60000),event(points[1]!.code,'0190b9a8-7000-7000-8000-000000000002','prod-user',now-30000)]
  const testEvents=[event(points[0]!.code,'0190b9a8-7000-7000-8000-000000000003','test-user',now-60000),event(points[1]!.code,'0190b9a8-7000-7000-8000-000000000004','test-user',now-30000)]
  for(const [path,events] of [['test-events',testEvents],['events',production]] as const){const res=await request.post(`/api/v1/collect/${path}`,{headers:{'X-Write-Key':writeKey},data:{events}});expect(res.status(),await res.text()).toBe(202);expect((await res.json()).accepted).toBe(2)}
  const fq={project_id:projectId,start:new Date(now-120000).toISOString(),end:new Date(now-10000).toISOString(),window_hours:1,point_codes:points.map(p=>p.code),platform:'web',channel:''}
  await expect.poll(async()=>{const res=await request.post('/api/v1/statistics/funnel',{headers,data:fq});if(!res.ok())return `HTTP ${res.status()} ${await res.text()}`;return (await res.json()).steps.map((s:{visitors:number})=>s.visitors)}, {timeout:30000}).toEqual([1,1])
  await page.goto('/login');await page.getByLabel(/账号/).fill('admin');await page.getByLabel(/密码/).fill(password);await page.getByRole('button',{name:/登录/}).click();await page.waitForURL(/\/projects\/\d+\/overview/);await page.goto(`/projects/${projectId}/requirements`)
  await page.getByRole('button',{name:'订单转化需求'}).click();await expect(page.getByText('view',{exact:true})).toBeVisible();await page.getByRole('button',{name:'查看验收'}).click();await expect(page.getByText(/验收通过/)).toBeVisible()
  await page.getByLabel('标题',{exact:true}).fill('页面创建的埋点需求')
  await page.getByLabel('目标',{exact:true}).fill('验证页面提交')
  await page.getByLabel('业务场景',{exact:true}).fill('订单页面')
  await page.getByRole('button',{name:'创建需求'}).click()
  await expect(page.getByRole('heading',{name:'页面创建的埋点需求'})).toBeVisible()
  await page.getByRole('combobox',{name:'点位',exact:true}).selectOption(String(points[1]!.id))
  await page.getByRole('combobox',{name:'负责人',exact:true}).selectOption(String(session.user.id))
  await page.getByRole('button',{name:'添加任务'}).click()
  await expect(page.getByText('order',{exact:true})).toBeVisible()
  await page.getByRole('button',{name:'查看验收'}).click()
  await page.getByLabel('说明',{exact:true}).fill('页面验收')
  await page.getByRole('button',{name:'记录验收'}).click()
  await expect(page.getByText(/页面验收/)).toBeVisible()
  await page.goto(`/projects/${projectId}/points/${points[0]!.id}`);await page.getByRole('button',{name:'查询 / 刷新'}).click();await expect(page.getByRole('cell',{name:'通过',exact:true})).toHaveCount(3)
  await page.goto(`/projects/${projectId}/statistics`);await expect(page.getByRole('heading',{name:'转化漏斗'})).toBeVisible();await expect(page.getByRole('heading',{name:'多点位趋势对比'})).toBeVisible()
  await page.getByRole('combobox',{name:'第 1 步'}).selectOption(points[0]!.code)
  await page.getByRole('combobox',{name:'第 2 步'}).selectOption(points[1]!.code)
  await page.getByRole('button',{name:'查询漏斗'}).click()
  await expect(page.getByRole('row').filter({hasText:points[1]!.code}).getByRole('cell',{name:'1',exact:true})).toBeVisible()
  await expect(page.getByText('100.00%')).toBeVisible()
  const funnelPanel=page.locator('article').filter({has:page.getByRole('heading',{name:'转化漏斗'})})
  await funnelPanel.getByLabel('图表名称').fill('订单漏斗图')
  await funnelPanel.getByRole('button',{name:'保存图表'}).click()
  await expect(funnelPanel.getByRole('option',{name:'订单漏斗图'})).toHaveCount(1)
  await funnelPanel.getByRole('button',{name:'加载图表配置'}).click()
  await expect(funnelPanel.getByRole('combobox',{name:'第 1 步'})).toHaveValue(points[0]!.code)
  const trendPanel=page.locator('article').filter({has:page.getByRole('heading',{name:'多点位趋势对比'})})
  await trendPanel.getByLabel('对比点位').selectOption(points.map(p=>p.code))
  await trendPanel.getByLabel('图表名称').fill('订单趋势图')
  await trendPanel.getByRole('button',{name:'保存图表'}).click()
  await expect(trendPanel.getByRole('option',{name:'订单趋势图'})).toHaveCount(1)
  await trendPanel.getByRole('button',{name:'加载图表配置'}).click()
  const viewerCharts=await request.get(`/api/v1/projects/${projectId}/charts?kind=TREND`,{headers:viewerHeaders})
  expect(viewerCharts.status()).toBe(200);const savedCharts=await viewerCharts.json() as Array<{id:number}>
  expect(savedCharts).toHaveLength(1)
  expect((await request.delete(`/api/v1/projects/${projectId}/charts/${savedCharts[0]!.id}`,{headers:viewerHeaders})).status()).toBe(403)
})
