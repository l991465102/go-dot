<script setup lang="ts">
import { computed } from 'vue'
import type { Category, PointDraft, PointProperty } from '@/api/tracking'
import CodeExamples from './CodeExamples.vue'

const props = defineProps<{ modelValue: PointDraft; categories: Category[]; projectId: number }>()
const emit = defineEmits<{ 'update:modelValue': [PointDraft] }>()
function patch(value: Partial<PointDraft>) { emit('update:modelValue', { ...props.modelValue, ...value }) }

const categoryPath = computed(() => {
  const byID = new Map(props.categories.map((category) => [category.id, category]))
  const path: Category[] = []
  let current = byID.get(props.modelValue.category_id)
  const visited = new Set<number>()
  while (current && !visited.has(current.id)) {
    visited.add(current.id)
    path.unshift(current)
    current = current.parent_id ? byID.get(current.parent_id) : undefined
  }
  return path.map((category) => category.name).join(' / ') || '未选择'
})
function propertyConstraint(property: PointProperty) {
  if (property.type === 'ENUM') return `ENUM：${property.enum_values?.join('、') || '未配置枚举值'}`
  if (property.type === 'ARRAY') return `ARRAY<${property.array_item_type || '未配置'}>`
  return property.type || '未配置类型'
}
</script>

<template>
  <section class="wizard-form">
    <h2>协作与发布确认</h2>
    <label>产品负责人<input :value="modelValue.product_owner" @input="patch({ product_owner: ($event.target as HTMLInputElement).value })"></label>
    <label>开发负责人<input :value="modelValue.developer_owner" @input="patch({ developer_owner: ($event.target as HTMLInputElement).value })"></label>
    <label>需求地址<input :value="modelValue.requirement_url" @input="patch({ requirement_url: ($event.target as HTMLInputElement).value })"></label>
    <div class="publish-review">
      <h3>发布检查</h3>
      <dl>
        <div><dt>分类</dt><dd>{{ categoryPath }}</dd></div>
        <div><dt>触发规则</dt><dd>{{ modelValue.trigger_timing || '未填写' }}</dd></div>
        <div><dt>指标</dt><dd>{{ modelValue.count_pv ? 'PV ' : '' }}{{ modelValue.count_uv ? `UV（${modelValue.uv_mode}）` : '' }}</dd></div>
        <div><dt>平台</dt><dd>{{ modelValue.supported_platforms.join('、') || '未选择' }}</dd></div>
        <div><dt>负责人</dt><dd>{{ modelValue.product_owner || '-' }} / {{ modelValue.developer_owner || '-' }}</dd></div>
      </dl>
      <div class="publication-properties">
        <h4>属性契约（{{ modelValue.properties.length }}）</h4>
        <p v-if="!modelValue.properties.length" class="muted">无自定义属性</p>
        <table v-else><thead><tr><th>编码</th><th>名称</th><th>类型/约束</th><th>要求</th></tr></thead><tbody><tr v-for="property in modelValue.properties" :key="property.code"><td><code>{{ property.code }}</code></td><td>{{ property.name }}</td><td>{{ propertyConstraint(property) }}</td><td>{{ property.required ? '必填' : '可选' }}</td></tr></tbody></table>
      </div>
      <CodeExamples :project-id="projectId" :point-code="modelValue.code" :platform="modelValue.supported_platforms[0] || 'web'" :properties="modelValue.properties" />
    </div>
  </section>
</template>
