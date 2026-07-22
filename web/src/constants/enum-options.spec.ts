import { describe, expect, it } from 'vitest'
import {
  behaviorTypeOptions,
  categoryLevelOptions,
  definitionStateOptions,
  developmentStateOptions,
  platformOptions,
  projectRoleOptions,
  propertyTypeOptions,
  statisticsGranularityOptions,
  statisticsRangeOptions,
  timeBasisOptions,
  timezoneOptions,
  uvModeOptions,
} from './enum-options'

describe('enum options', () => {
  it('exposes Chinese labels while preserving protocol values', () => {
    expect(definitionStateOptions).toEqual([
      { value: 'DRAFT', label: '草稿' },
      { value: 'PUBLISHED', label: '已发布' },
      { value: 'DEPRECATED', label: '已废弃' },
    ])
    expect(developmentStateOptions.map(({ value, label }) => [value, label])).toEqual([
      ['PENDING', '待开发'], ['DEVELOPING', '开发中'], ['TESTING', '测试中'], ['ONLINE', '已上线'],
    ])
    expect(behaviorTypeOptions.map(({ value, label }) => [value, label])).toEqual([
      ['PAGE_VIEW', '页面浏览'], ['ELEMENT_EXPOSE', '元素曝光'], ['ELEMENT_CLICK', '元素点击'],
      ['BUSINESS_ACTION', '业务操作'], ['BUSINESS_RESULT', '业务结果'], ['CUSTOM', '自定义'],
    ])
    expect(categoryLevelOptions.map((option) => option.label)).toEqual(['业务域', '业务模块', '业务场景'])
    expect(platformOptions.map((option) => option.label)).toEqual(['Web', 'iOS', 'Android', '小程序', '服务端'])
    expect(propertyTypeOptions.map((option) => option.label)).toEqual(['字符串', '整数', '小数', '布尔值', '日期时间', '枚举', '数组'])
    expect(uvModeOptions).toEqual([
      { value: 'APPROXIMATE', label: '近似统计（HLL）' },
      { value: 'EXACT', label: '精确统计' },
    ])
    expect(timeBasisOptions.map((option) => option.value)).toEqual(['CLIENT', 'SERVER'])
    expect(projectRoleOptions).toEqual([
      { value: 'ADMIN', label: '管理员' },
      { value: 'VIEWER', label: '只读用户' },
    ])
    expect(timezoneOptions).toEqual([
      { value: 'Asia/Shanghai', label: '上海时区（UTC+8）' },
      { value: 'UTC', label: '协调世界时（UTC）' },
    ])
    expect(statisticsRangeOptions).toEqual([
      { value: 24, label: '24 小时' },
      { value: 168, label: '7 天' },
      { value: 720, label: '30 天' },
    ])
    expect(statisticsGranularityOptions).toEqual([
      { value: 'hour', label: '小时' },
      { value: 'day', label: '天' },
    ])
  })
})
