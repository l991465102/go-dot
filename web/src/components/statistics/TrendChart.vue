<script setup lang="ts">
import { init, use, type ECharts } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { TrendPoint } from '@/api/statistics'
const props = defineProps<{ items: TrendPoint[] }>()
const root = ref<HTMLElement>()
use([LineChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer])
let chart: ECharts | undefined
let observer: ResizeObserver | undefined
function draw() {
  if (!root.value) return
  chart ||= init(root.value)
  chart.setOption({ tooltip: { trigger: 'axis' }, legend: { data: ['PV', props.items[0]?.approximate ? 'UV（近似）' : 'UV（精确）'] }, xAxis: { type: 'category', data: props.items.map((item) => new Date(item.period).toLocaleString()) }, yAxis: { type: 'value' }, series: [{ name: 'PV', type: 'line', data: props.items.map((item) => item.pv) }, { name: props.items[0]?.approximate ? 'UV（近似）' : 'UV（精确）', type: 'line', data: props.items.map((item) => item.uv) }] })
}
onMounted(async () => { await nextTick(); draw(); observer = new ResizeObserver(() => chart?.resize()); if (root.value) observer.observe(root.value) })
watch(() => props.items, draw, { deep: true })
onBeforeUnmount(() => { observer?.disconnect(); chart?.dispose(); chart = undefined })
</script>
<template><div ref="root" class="trend-chart" role="img" aria-label="PV 与 UV 趋势图"></div></template>
