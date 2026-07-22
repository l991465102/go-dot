import type { CreateProjectTimezone, ProjectRole } from '@/api/projects'
import type { StatisticsGranularity, StatisticsRangeHours } from '@/api/statistics'
import type {
  BehaviorType,
  CategoryLevel,
  DefinitionState,
  DevelopmentState,
  Platform,
  PointProperty,
  PropertyType,
  TimeBasis,
  UVMode,
} from '@/api/tracking'

export interface EnumOption<T extends string | number> {
  readonly value: T
  readonly label: string
}

type Labels<T extends string | number> = { readonly [Value in T]: string }
type ArrayItemType = NonNullable<PointProperty['array_item_type']>

function stringOptions<T extends string>(labels: Labels<T>): readonly EnumOption<T>[] {
  return Object.entries(labels).map(([value, label]) => ({ value: value as T, label: label as string }))
}

function numberOptions<T extends number>(labels: Labels<T>): readonly EnumOption<T>[] {
  return Object.entries(labels).map(([value, label]) => ({ value: Number(value) as T, label: label as string }))
}

export const definitionStateOptions = stringOptions<DefinitionState>({
  DRAFT: '草稿',
  PUBLISHED: '已发布',
  DEPRECATED: '已废弃',
})

export const developmentStateOptions = stringOptions<DevelopmentState>({
  PENDING: '待开发',
  DEVELOPING: '开发中',
  TESTING: '测试中',
  ONLINE: '已上线',
})

export const behaviorTypeOptions = stringOptions<BehaviorType>({
  PAGE_VIEW: '页面浏览',
  ELEMENT_EXPOSE: '元素曝光',
  ELEMENT_CLICK: '元素点击',
  BUSINESS_ACTION: '业务操作',
  BUSINESS_RESULT: '业务结果',
  CUSTOM: '自定义',
})

export const categoryLevelOptions = stringOptions<CategoryLevel>({
  DOMAIN: '业务域',
  MODULE: '业务模块',
  SCENE: '业务场景',
})

export const platformOptions = stringOptions<Platform>({
  web: 'Web',
  ios: 'iOS',
  android: 'Android',
  mini_program: '小程序',
  server: '服务端',
})

export const propertyTypeOptions = stringOptions<PropertyType>({
  STRING: '字符串',
  INTEGER: '整数',
  DECIMAL: '小数',
  BOOLEAN: '布尔值',
  DATETIME: '日期时间',
  ENUM: '枚举',
  ARRAY: '数组',
})

export const arrayItemTypeOptions = stringOptions<ArrayItemType>({
  STRING: '字符串',
  INTEGER: '整数',
  DECIMAL: '小数',
  BOOLEAN: '布尔值',
  DATETIME: '日期时间',
})

export const uvModeOptions = stringOptions<UVMode>({
  APPROXIMATE: '近似统计（HLL）',
  EXACT: '精确统计',
})

export const timeBasisOptions = stringOptions<TimeBasis>({
  CLIENT: '客户端事件时间',
  SERVER: '服务端接收时间',
})

export const projectRoleOptions = stringOptions<ProjectRole>({
  ADMIN: '管理员',
  VIEWER: '只读用户',
})

export const timezoneOptions = stringOptions<CreateProjectTimezone>({
  'Asia/Shanghai': '上海时区（UTC+8）',
  UTC: '协调世界时（UTC）',
})

export const statisticsRangeOptions = numberOptions<StatisticsRangeHours>({
  24: '24 小时',
  168: '7 天',
  720: '30 天',
})

export const statisticsGranularityOptions = stringOptions<StatisticsGranularity>({
  hour: '小时',
  day: '天',
})
