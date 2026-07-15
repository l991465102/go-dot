<script setup lang="ts">
import type { StatisticsSummary } from '@/api/statistics'
defineProps<{ realtime?: StatisticsSummary; realtimePoint?: string; invalidCount: number; deadLetterCount: number; loading?: boolean }>()
</script>
<template>
  <div class="metric-card-grid" :aria-busy="loading">
    <article class="overview-card"><p>实时 PV</p><strong>{{ realtime?.pv ?? '—' }}</strong><small>Redis · {{ realtimePoint || '暂无已发布点位' }} · 最近完整小时</small></article>
    <article class="overview-card"><p>实时 UV{{ realtime ? (realtime.approximate ? '（近似）' : '（精确）') : '' }}</p><strong>{{ realtime?.uv ?? '—' }}</strong><small>Redis · {{ realtimePoint || '请选择点位' }}</small></article>
    <article class="overview-card"><p>无效事件</p><strong>{{ invalidCount }}</strong><small>最近 24 小时</small></article>
    <article class="overview-card"><p>死信消息</p><strong>{{ deadLetterCount }}</strong><small>项目当前总数</small></article>
  </div>
</template>
