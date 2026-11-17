<script setup lang="ts">
import { ref, watch } from 'vue'
import { useProjectStore } from '@/stores/project'
import { createChart, deleteChart, listCharts, type ChartConfig, type ChartKind, type SavedChart } from '@/api/charts'
const props=defineProps<{projectId:number;kind:ChartKind;config:ChartConfig}>(),emit=defineEmits<{load:[config:ChartConfig]}>()
const store=useProjectStore(),charts=ref<SavedChart[]>([]),selected=ref<number>(),name=ref(''),error=ref(''),busy=ref(false)
async function load(){const id=props.projectId;error.value='';try{const result=await listCharts(id,props.kind);if(id===props.projectId)charts.value=result}catch{if(id===props.projectId)error.value='图表列表加载失败。'}}
watch(()=>[props.projectId,props.kind],()=>{charts.value=[];selected.value=undefined;void load()},{immediate:true})
function apply(){const item=charts.value.find(c=>c.id===selected.value);if(item)emit('load',{...item.config,point_codes:[...item.config.point_codes]})}
async function save(){if(!name.value.trim())return;busy.value=true;error.value='';const project=props.projectId;try{const {id}=await createChart(project,props.kind,name.value.trim(),props.config);if(project!==props.projectId)return;name.value='';await load();selected.value=id}catch{if(project===props.projectId)error.value='保存失败，请检查名称、点位和时间范围。'}finally{busy.value=false}}
async function remove(){const id=selected.value;if(!id||!window.confirm('确定删除此图表配置？'))return;busy.value=true;const project=props.projectId;try{await deleteChart(project,id);if(project===props.projectId){selected.value=undefined;await load()}}catch{if(project===props.projectId)error.value='删除图表失败。'}finally{busy.value=false}}
</script>
<template>
  <section class="saved-charts" aria-label="图表配置">
    <div class="saved-charts__heading"><div><strong>图表配置</strong><small>保存常用筛选，方便下次快速查看</small></div><span>最多显示最近 200 个</span></div>
    <div class="saved-charts__controls"><label class="analytics-field">已保存图表<select v-model.number="selected"><option :value="undefined">选择已保存的图表</option><option v-for="c in charts" :key="c.id" :value="c.id">{{ c.name }}</option></select></label><button type="button" class="secondary-button" :disabled="!selected" @click="apply">加载配置</button><template v-if="store.canMutate"><label class="analytics-field">图表名称<input v-model="name" maxlength="128" placeholder="为当前配置命名"></label><button type="button" class="secondary-button" :disabled="busy||!name.trim()" @click="save">保存图表</button><button type="button" class="analytics-text-button" :disabled="busy||!selected" @click="remove">删除</button></template></div>
    <p class="analytics-hint">保存固定时间范围与筛选条件，加载后需重新查询。</p><p v-if="error" class="analytics-error" role="alert">{{ error }}</p>
  </section>
</template>
