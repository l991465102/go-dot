<script setup lang="ts">
import { ref, watch } from 'vue'
import { apiClient } from '@/api/client'
import { formatShanghaiDateTime } from '@/utils/datetime'
const props=defineProps<{projectId:number;requirementId:number;taskId:number;canMutate:boolean}>()
type Acceptance={id:number;result:string;note:string;actor_id:number;created_at:string}
const items=ref<Acceptance[]>([]), result=ref('PASS'), note=ref(''),error=ref(''),busy=ref(false),loaded=ref(false)
const url=()=>`/projects/${props.projectId}/requirements/${props.requirementId}/tasks/${props.taskId}/acceptances`
watch(()=>[props.projectId,props.requirementId,props.taskId],()=>{items.value=[];loaded.value=false;error.value=''})
async function load(){error.value='';try{items.value=(await apiClient.get<Acceptance[]>(url())).data;loaded.value=true}catch{error.value='加载验收记录失败'}}
async function save(){busy.value=true;error.value='';try{await apiClient.post(url(),{result:result.value,note:note.value});note.value='';await load()}catch{error.value='保存验收记录失败'}finally{busy.value=false}}
</script>
<template><div><button type="button" @click="load">查看验收</button><p v-if="error" role="alert">{{ error }}</p><div v-if="loaded"><p v-if="!items.length">暂无验收记录</p><ul v-else><li v-for="a in items" :key="a.id">{{ a.result === 'PASS' ? '通过' : '未通过' }} · {{ formatShanghaiDateTime(a.created_at) }} · 验收人 {{ a.actor_id }} · {{ a.note || '无说明' }}</li></ul><form v-if="canMutate" @submit.prevent="save"><label>验收结果<select v-model="result"><option value="PASS">通过</option><option value="FAIL">未通过</option></select></label><label>说明<input v-model="note" maxlength="2000"></label><button :disabled="busy">记录验收</button></form></div></div></template>
