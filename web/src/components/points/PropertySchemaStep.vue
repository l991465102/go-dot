<script setup lang="ts">
import type { MetricAggregation, PointDraft, PointProperty, PropertyType, PropertyAnalyticsMode } from '@/api/tracking'
import { arrayItemTypeOptions, metricAggregationOptions, propertyAnalyticsModeOptions, propertyTypeOptions } from '@/constants/enum-options'
defineProps<{ modelValue: PointDraft }>(); const emit=defineEmits<{ 'update:modelValue':[PointDraft] }>()
function add(m:PointDraft){emit('update:modelValue',{...m,properties:[...m.properties,{code:'',name:'',type:'STRING',required:false,analytics:{mode:'NONE'},sort_order:m.properties.length}]})}
function update(m:PointDraft,i:number,v:Partial<PointProperty>){const p=m.properties.slice();p[i]={...p[i]!,...v};emit('update:modelValue',{...m,properties:p})}
function remove(m:PointDraft,i:number){emit('update:modelValue',{...m,properties:m.properties.filter((_,x)=>x!==i)})}
function allowedModes(type: PropertyType|'') {
  if (type === 'INTEGER' || type === 'DECIMAL') return propertyAnalyticsModeOptions.filter((option) => option.value !== 'DIMENSION')
  if (type === 'STRING' || type === 'ENUM' || type === 'BOOLEAN') return propertyAnalyticsModeOptions.filter((option) => option.value !== 'METRIC')
  return propertyAnalyticsModeOptions.filter((option) => option.value === 'NONE')
}
function changeType(m:PointDraft,i:number,type:PropertyType) {
  const mode = allowedModes(type).some((option) => option.value === m.properties[i]?.analytics?.mode) ? m.properties[i]?.analytics?.mode ?? 'NONE' : 'NONE'
  update(m,i,{type,analytics:{mode,aggregations:mode==='METRIC' ? m.properties[i]?.analytics?.aggregations ?? ['COUNT'] : undefined}})
}
function changeMode(m:PointDraft,i:number,mode:PropertyAnalyticsMode) {
  update(m,i,{analytics:{mode,aggregations:mode==='METRIC' ? ['COUNT'] : undefined}})
}
function toggleAggregation(m:PointDraft,i:number,aggregation:MetricAggregation,checked:boolean) {
  const current = m.properties[i]?.analytics?.aggregations ?? []
  const aggregations = checked ? [...new Set([...current,aggregation])] : current.filter((item) => item !== aggregation)
  update(m,i,{analytics:{mode:'METRIC',aggregations}})
}
</script>
<template>
  <section>
    <div class="section-heading"><h2>属性 Schema</h2><button type="button" class="secondary-button" @click="add(modelValue)">新增属性</button></div>
    <p class="field-help">只为确实需要报表分析的属性开启统计。字符串维度按天展示前 50 项，其余归入“其他”。</p>
    <div v-if="!modelValue.properties.length" class="empty-state">暂无自定义属性</div>
    <div v-for="(p,i) in modelValue.properties" :key="i" class="property-row property-analytics-row">
      <input :value="p.code" placeholder="属性编码" @input="update(modelValue,i,{code:($event.target as HTMLInputElement).value})">
      <input :value="p.name" placeholder="中文名称" @input="update(modelValue,i,{name:($event.target as HTMLInputElement).value})">
      <select :value="p.type" aria-label="属性类型" @change="changeType(modelValue,i,($event.target as HTMLSelectElement).value as PropertyType)">
        <option v-for="option in propertyTypeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
      </select>
      <label><input type="checkbox" :checked="p.required" @change="update(modelValue,i,{required:($event.target as HTMLInputElement).checked})">必填</label>
      <input v-if="p.type==='ENUM'" :value="p.enum_values?.join(',')" placeholder="枚举值，逗号分隔" @input="update(modelValue,i,{enum_values:($event.target as HTMLInputElement).value.split(',').map(v=>v.trim()).filter(Boolean)})">
      <select v-if="p.type==='ARRAY'" :value="p.array_item_type" @change="update(modelValue,i,{array_item_type:($event.target as HTMLSelectElement).value as PointProperty['array_item_type']})"><option value="">元素类型</option><option v-for="option in arrayItemTypeOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select>
      <select :value="p.analytics?.mode ?? 'NONE'" aria-label="统计方式" @change="changeMode(modelValue,i,($event.target as HTMLSelectElement).value as PropertyAnalyticsMode)">
        <option v-for="option in allowedModes(p.type)" :key="option.value" :value="option.value">{{ option.label }}</option>
      </select>
      <div v-if="p.analytics?.mode==='METRIC'" class="aggregation-options">
        <label v-for="option in metricAggregationOptions" :key="option.value">
          <input type="checkbox" :checked="p.analytics?.aggregations?.includes(option.value)" @change="toggleAggregation(modelValue,i,option.value,($event.target as HTMLInputElement).checked)">
          {{ option.label }}
        </label>
      </div>
      <span v-else-if="p.analytics?.mode==='DIMENSION'" class="field-help">固定统计：事件数、UV、占比</span>
      <button type="button" class="danger-link" @click="remove(modelValue,i)">删除</button>
    </div>
  </section>
</template>
