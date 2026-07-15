<script setup lang="ts">
import { computed } from 'vue'
import { useProjectStore } from '@/stores/project'

const projects = useProjectStore()
const project = computed(() => projects.currentProject)
</script>

<template>
  <section v-loading="projects.loading" aria-labelledby="overview-title">
    <header class="page-heading">
      <div>
        <p class="eyebrow">PROJECT OVERVIEW</p>
        <h1 id="overview-title">项目概览</h1>
        <p>快速了解 {{ project?.name }} 的数据接入与运行状态。</p>
      </div>
      <el-tag v-if="project" type="success" effect="light">{{ project.active ? '运行中' : '已停用' }}</el-tag>
    </header>
    <div class="overview-grid">
      <article class="overview-card overview-card--hero">
        <p class="eyebrow">项目标识</p><strong>{{ project?.code }}</strong>
        <dl><div><dt>时区</dt><dd>{{ project?.timezone }}</dd></div><div><dt>权限</dt><dd>{{ projects.canMutate ? '管理员' : '只读成员' }}</dd></div></dl>
      </article>
      <article class="overview-card"><span class="metric-icon metric-icon--blue">点</span><p>点位定义</p><strong>—</strong><small>进入点位管理查看</small></article>
      <article class="overview-card"><span class="metric-icon metric-icon--green">数</span><p>今日事件</p><strong>—</strong><small>统计数据接入后展示</small></article>
      <article class="overview-card"><span class="metric-icon metric-icon--orange">诊</span><p>数据质量</p><strong>—</strong><small>诊断服务正常</small></article>
    </div>
    <article class="getting-started">
      <div><p class="eyebrow">GETTING STARTED</p><h2>开始接入业务数据</h2><p>先定义业务分类与点位，再由客户端按发布后的点位编码上报事件。</p></div>
      <ol><li><span>1</span>创建业务分类</li><li><span>2</span>定义并发布点位</li><li><span>3</span>客户端接入上报</li><li><span>4</span>查看统计与诊断</li></ol>
    </article>
  </section>
</template>
