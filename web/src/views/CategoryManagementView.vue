<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useProjectStore } from '@/stores/project'
import * as api from '@/api/tracking'
import type { Category, CategoryInput, CategoryLevel } from '@/api/tracking'

const route = useRoute(), projects = useProjectStore()
const projectId = computed(() => Number(route.params.projectId)), canMutate = computed(() => projects.canMutate)
const items = ref<Category[]>([]), loading = ref(false), error = ref(''), editing = ref<Category>(), dialog = ref(false)
let controller: AbortController | undefined
const form = ref<CategoryInput>({ code: '', name: '', level: 'DOMAIN', sort_order: 0 })
const parents = computed(() => items.value.filter((c) => c.active && (form.value.level === 'MODULE' ? c.level === 'DOMAIN' : form.value.level === 'SCENE' ? c.level === 'MODULE' : false)))

async function load() { controller?.abort(); const active = new AbortController(); controller = active; loading.value = true; error.value = ''; try { items.value = await api.listCategories(projectId.value, active.signal) } catch (cause) { if ((cause as { name?: string }).name !== 'CanceledError') error.value = cause instanceof Error ? cause.message : '分类加载失败' } finally { if (controller === active) loading.value = false } }
watch(projectId, () => { dialog.value = false; editing.value = undefined; void load() }, { immediate: true })
onBeforeUnmount(() => controller?.abort())
function open(item?: Category) { editing.value = item; form.value = item ? { code: item.code, name: item.name, level: item.level, parent_id: item.parent_id, description: item.description, sort_order: item.sort_order } : { code: '', name: '', level: 'DOMAIN', sort_order: 0 }; dialog.value = true }
async function save() { error.value = ''; try { if (editing.value) await api.updateCategory(projectId.value, editing.value.id, { name: form.value.name, description: form.value.description, sort_order: form.value.sort_order, active: editing.value.active }); else await api.createCategory(projectId.value, form.value); dialog.value = false; await load() } catch (cause) { error.value = cause instanceof Error ? cause.message : '保存失败' } }
async function toggle(item: Category) { if (!confirm(item.active ? '确定停用该分类？已有子分类、草稿或已发布点位时后端会拒绝。' : '确定启用该分类？')) return; try { await api.updateCategory(projectId.value, item.id, { name: item.name, description: item.description, sort_order: item.sort_order, active: !item.active }); await load() } catch (cause) { error.value = cause instanceof Error ? cause.message : '状态更新失败' } }
function changeLevel(value: string) { form.value.level = value as CategoryLevel; form.value.parent_id = undefined }
</script>

<template>
  <section>
    <header class="page-heading"><div><p class="eyebrow">Taxonomy</p><h1>业务分类</h1><p>固定三级结构：业务域 Domain → 模块 Module → 场景 Scene。</p></div><button v-if="canMutate" class="primary-button" @click="open()">新增分类</button></header>
    <div v-if="error" class="form-alert" role="alert">{{ error }}</div><div v-if="loading" class="empty-state">正在加载分类…</div>
    <div v-else class="category-grid">
      <article v-for="domain in items.filter(c => c.level === 'DOMAIN')" :key="domain.id" class="category-card">
        <header><div><strong>{{ domain.name }}</strong><small>{{ domain.code }}</small></div><span :class="['state-chip', { muted: !domain.active }]">{{ domain.active ? '启用' : '停用' }}</span></header>
        <div v-if="canMutate" class="category-actions"><button @click="open(domain)">重命名/排序</button><button @click="toggle(domain)">{{ domain.active ? '停用' : '启用' }}</button></div>
        <div v-for="module in items.filter(c => c.parent_id === domain.id)" :key="module.id" class="category-branch">
          <strong>{{ module.name }}</strong><small>{{ module.code }}</small><div v-if="canMutate" class="category-actions"><button @click="open(module)">编辑</button><button @click="toggle(module)">{{ module.active ? '停用' : '启用' }}</button></div>
          <div class="scene-list"><span v-for="scene in items.filter(c => c.parent_id === module.id)" :key="scene.id" :class="{ disabled: !scene.active }">{{ scene.name }} <small>{{ scene.code }}</small><button v-if="canMutate" aria-label="编辑场景" @click="open(scene)">编辑</button><button v-if="canMutate" :aria-label="scene.active ? '停用场景' : '启用场景'" @click="toggle(scene)">{{ scene.active ? '停用' : '启用' }}</button></span></div>
        </div>
      </article>
    </div>
    <div v-if="dialog" class="modal-backdrop"><form class="modal-card wizard-form" role="dialog" aria-modal="true" @submit.prevent="save"><h2>{{ editing ? '编辑分类' : '新增分类' }}</h2><label>层级<select :value="form.level" :disabled="!!editing" @change="changeLevel(($event.target as HTMLSelectElement).value)"><option>DOMAIN</option><option>MODULE</option><option>SCENE</option></select></label><label v-if="form.level !== 'DOMAIN'">父分类<select v-model.number="form.parent_id" :disabled="!!editing"><option :value="undefined">请选择</option><option v-for="p in parents" :key="p.id" :value="p.id">{{ p.name }}</option></select></label><label>编码<input v-model="form.code" :disabled="!!editing" required></label><label>名称<input v-model="form.name" required></label><label>排序<input v-model.number="form.sort_order" type="number"></label><label>说明<textarea v-model="form.description" /></label><footer class="wizard-actions"><button type="button" class="secondary-button" @click="dialog = false">取消</button><button class="primary-button" type="submit">保存</button></footer></form></div>
  </section>
</template>
