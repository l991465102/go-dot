<script setup lang="ts">
import { ref, watch } from 'vue'
import { apiClient } from '@/api/client'
import { formatShanghaiDateTime } from '@/utils/datetime'
const props=defineProps<{projectId:number; pointCode:string}>()
type Row={point_code:string;platform:string;app_version:string;status:string;code:string;received_at:string}
const rows=ref<Row[]>([]), loading=ref(false), error=ref(''), loaded=ref(false)
watch(()=>[props.projectId,props.pointCode],()=>{rows.value=[];loaded.value=false;error.value=''})
async function load(){const project=props.projectId,code=props.pointCode;loading.value=true;error.value='';try{const result=await apiClient.get<Row[]>(`/projects/${project}/test-events`,{params:{point_code:code}});if(project===props.projectId&&code===props.pointCode){rows.value=result.data;loaded.value=true}}catch{error.value='测试记录加载失败'}finally{loading.value=false}}
</script>
<template><article class="detail-card detail-card--wide"><h2>测试上报记录</h2><p>使用 <code>/api/v1/collect/test-events</code> 上报，校验结果不进入正式统计。展示最近七天的脱敏记录。</p><button type="button" :disabled="loading" @click="load">{{ loading ? '加载中' : '查询 / 刷新' }}</button><p v-if="error" role="alert">{{ error }}</p><p v-else-if="loaded && !rows.length">暂无记录</p><table v-else-if="rows.length"><thead><tr><th>接收时间</th><th>平台</th><th>版本</th><th>校验</th><th>错误码</th></tr></thead><tbody><tr v-for="(row,i) in rows" :key="i"><td>{{ formatShanghaiDateTime(row.received_at) }}</td><td>{{ row.platform }}</td><td>{{ row.app_version || '-' }}</td><td>{{ row.status === 'accepted' ? '通过' : '未通过' }}</td><td>{{ row.code || '-' }}</td></tr></tbody></table></article></template>
