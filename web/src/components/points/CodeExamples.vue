<script setup lang="ts">
import { computed, ref } from 'vue'
import type { PointProperty } from '@/api/tracking'
const props=defineProps<{ projectId:number; pointCode:string; platform:string; properties:PointProperty[] }>(); const tab=ref<'js'|'go'|'curl'>('js')
function fallback(p: PointProperty) { if (p.type==='BOOLEAN') return true; if (p.type==='INTEGER') return 1; if (p.type==='DECIMAL') return 1.5; if (p.type==='ARRAY') return []; return p.enum_values?.[0] ?? '示例值' }
const values=computed(()=>Object.fromEntries(props.properties.map(p=>[p.code,p.example_value ?? fallback(p)])))
const event=computed(()=>({event_id:'0190b9a8-7000-7000-8000-000000000001',point_code:props.pointCode,event_time:1784012400123,anonymous_id:'device-123',platform:props.platform,properties:values.value}))
const examples=computed(()=>({
  js:`await fetch('/api/v1/collect/events', {\n  method: 'POST',\n  headers: { 'Content-Type': 'application/json', 'X-Write-Key': 'YOUR_WRITE_KEY' },\n  body: JSON.stringify(${JSON.stringify({project_id:props.projectId,events:[event.value]},null,2)})\n})`,
  go:`payload := map[string]any{"project_id": ${props.projectId}, "events": []any{map[string]any{"point_code": "${props.pointCode}", "event_time": time.Now().UnixMilli(), "anonymous_id": "device-123", "platform": "${props.platform}", "properties": map[string]any${JSON.stringify(values.value)}}}}\n// POST /api/v1/collect/events with X-Write-Key`,
  curl:`curl -X POST http://localhost:8080/api/v1/collect/events \\\n  -H 'Content-Type: application/json' \\\n  -H 'X-Write-Key: YOUR_WRITE_KEY' \\\n  -d '${JSON.stringify({project_id:props.projectId,events:[event.value]})}'`,
}))
</script>
<template><div class="code-examples"><div class="code-tabs"><button v-for="key in (['js','go','curl'] as const)" :key="key" type="button" :class="{active:tab===key}" @click="tab=key">{{ key==='js'?'JavaScript':key==='go'?'Go':'cURL' }}</button></div><pre><code>{{ examples[tab] }}</code></pre></div></template>
