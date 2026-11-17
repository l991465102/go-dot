<script setup lang="ts">
import SavedCharts from './SavedCharts.vue'
import type { ChartConfig } from '@/api/charts'
import { computed, ref, watch } from 'vue'
import { apiClient } from '@/api/client'
import { listPoints, type Point } from '@/api/tracking'
import { formatShanghaiDateTime } from '@/utils/datetime'
const props=defineProps<{projectId:number}>()
type Result={steps:{point_code:string;visitors:number;conversion:number|null}[];incomplete:boolean;evaluated_until:string}
const now=Date.now(), start=ref(localTime(new Date(now-86400000))),end=ref(localTime(new Date(now))),windowHours=ref(24),codes=ref(['','']),platform=ref(''),channel=ref(''),points=ref<Point[]>([]),result=ref<Result>(),error=ref(''),loading=ref(false)
const chartConfig=computed<ChartConfig>(()=>({point_codes:[...codes.value],start:new Date(start.value).toISOString(),end:new Date(end.value).toISOString(),window_hours:windowHours.value,platform:platform.value,channel:channel.value}))
function applyChart(c:ChartConfig){queryVersion++;loading.value=false;start.value=localTime(new Date(c.start));end.value=localTime(new Date(c.end));codes.value=[...c.point_codes];windowHours.value=c.window_hours||24;platform.value=c.platform||'';channel.value=c.channel||'';result.value=undefined}
const usable=computed(()=>points.value.filter(p=>p.definition_state!=='DRAFT'))
function localTime(date:Date){return new Date(date.getTime()-date.getTimezoneOffset()*60000).toISOString().slice(0,16)}
let queryVersion=0
async function loadPoints(){queryVersion++;loading.value=false;error.value='';const project=props.projectId;result.value=undefined;codes.value=['',''];points.value=[];try{const first=await listPoints(project,{page:1,page_size:100});let items=[...first.items];for(let page=2;items.length<first.total;page++){const next=await listPoints(project,{page,page_size:100});if(!next.items.length)break;items.push(...next.items)}if(project===props.projectId)points.value=items}catch{if(project===props.projectId)error.value='点位列表加载失败'}}
watch(()=>props.projectId,loadPoints,{immediate:true})
async function query(){error.value='';result.value=undefined;const from=new Date(start.value),to=new Date(end.value);if(!Number.isFinite(from.getTime())||!Number.isFinite(to.getTime())||codes.value.some(v=>!v)||new Set(codes.value).size!==codes.value.length){error.value='请填写有效时间并选择不同的点位。';return}loading.value=true;const version=++queryVersion,project=props.projectId;try{const response=await apiClient.post<Result>('/statistics/funnel',{project_id:project,start:from.toISOString(),end:to.toISOString(),window_hours:windowHours.value,point_codes:codes.value,platform:platform.value,channel:channel.value});if(version===queryVersion)result.value=response.data}catch{if(version===queryVersion)error.value='漏斗查询失败，请检查时间范围、点位和项目权限。'}finally{if(version===queryVersion)loading.value=false}}
</script>
<template>
  <article class="overview-card analytics-card">
    <header class="analytics-card__header"><div><span class="analytics-card__eyebrow">FUNNEL ANALYSIS</span><h2>转化漏斗</h2><p>按步骤查看用户从开始到转化的过程。</p></div><span class="analytics-card__badge">{{ codes.length }} 个步骤</span></header>
    <SavedCharts v-if="start && end && Number.isFinite(new Date(start).getTime()) && Number.isFinite(new Date(end).getTime())" :project-id="projectId" kind="FUNNEL" :config="chartConfig" @load="applyChart" />
    <form class="analytics-form funnel-form" @submit.prevent="query">
      <label class="analytics-field">首步开始<input v-model="start" type="datetime-local" required></label><label class="analytics-field">首步结束<input v-model="end" type="datetime-local" required></label><label class="analytics-field">转化窗口（小时）<input v-model.number="windowHours" type="number" min="1" max="168" required></label>
      <label v-for="(_,i) in codes" :key="i" class="analytics-field">第 {{ i+1 }} 步<select v-model="codes[i]" required><option value="" disabled>请选择点位</option><option v-for="p in usable" :key="p.id" :value="p.code">{{ p.name }}（{{ p.code }}）</option></select></label>
      <div class="analytics-form__inline"><button v-if="codes.length<5" type="button" class="secondary-button" @click="codes.push('')">+ 增加步骤</button><button v-if="codes.length>2" type="button" class="analytics-text-button" @click="codes.pop()">移除最后一步</button></div>
      <label class="analytics-field">平台（可选）<input v-model="platform" maxlength="32" placeholder="例如 web"></label><label class="analytics-field">渠道（可选）<input v-model="channel" maxlength="128" placeholder="输入渠道编码"></label><div class="analytics-form__action"><button class="primary-button" :disabled="loading">{{ loading ? '查询中…' : '查询漏斗' }}</button></div>
    </form>
    <p class="analytics-hint">首步需在所选区间内，后续步骤按时间顺序发生；同一时间的事件不计为先后。匿名与登录身份不会自动合并。可查最近 21 天内、最长 7 天的首步区间；时间使用浏览器本地时区。</p>
    <p v-if="error" class="analytics-error" role="alert">{{ error }}</p><template v-if="result"><p v-if="result.incomplete" class="analytics-notice">部分首步的转化窗口尚未结束，结果可能继续增长。</p><p class="analytics-hint">数据计算截至 {{ formatShanghaiDateTime(result.evaluated_until) }}（上海时间），数据写入可能延迟。</p><div class="analytics-table-wrap"><table class="analytics-table"><thead><tr><th>步骤</th><th>点位</th><th>访客数</th><th>相对上一步转化</th></tr></thead><tbody><tr v-for="(step,i) in result.steps" :key="step.point_code"><td>{{ i+1 }}</td><td>{{ step.point_code }}</td><td>{{ step.visitors }}</td><td>{{ step.conversion == null ? '—' : `${(step.conversion*100).toFixed(2)}%` }}</td></tr></tbody></table></div></template>
  </article>
</template>
