import type { ProjectRole } from '@/api/projects'
import type {
  BehaviorType,
  CategoryLevel,
  DefinitionState,
  DevelopmentState,
  PropertyType,
} from '@/api/tracking'

export interface EnumOption<T extends string> {
  readonly value: T
  readonly label: string
}

export type Platform = 'web' | 'ios' | 'android' | 'mini_program' | 'server'
export type UVMode = 'APPROXIMATE' | 'EXACT'
export type TimeBasis = 'CLIENT' | 'SERVER'
export type SupportedTimezone = 'Asia/Shanghai' | 'UTC'
export type ArrayItemType = Exclude<PropertyType, 'ENUM' | 'ARRAY'>

export const definitionStateOptions = [
  { value: 'DRAFT', label: '草稿' },
  { value: 'PUBLISHED', label: '已发布' },
  { value: 'DEPRECATED', label: '已废弃' },
] as const satisfies readonly EnumOption<DefinitionState>[]

export const developmentStateOptions = [
  { value: 'PENDING', label: '待开发' },
  { value: 'DEVELOPING', label: '开发中' },
  { value: 'TESTING', label: '测试中' },
  { value: 'ONLINE', label: '已上线' },
] as const satisfies readonly EnumOption<DevelopmentState>[]

export const behaviorTypeOptions = [
  { value: 'PAGE_VIEW', label: '页面浏览' },
  { value: 'ELEMENT_EXPOSE', label: '元素曝光' },
  { value: 'ELEMENT_CLICK', label: '元素点击' },
  { value: 'BUSINESS_ACTION', label: '业务操作' },
  { value: 'BUSINESS_RESULT', label: '业务结果' },
  { value: 'CUSTOM', label: '自定义' },
] as const satisfies readonly EnumOption<BehaviorType>[]

export const categoryLevelOptions = [
  { value: 'DOMAIN', label: '业务域' },
  { value: 'MODULE', label: '业务模块' },
  { value: 'SCENE', label: '业务场景' },
] as const satisfies readonly EnumOption<CategoryLevel>[]

export const platformOptions = [
  { value: 'web', label: 'Web' },
  { value: 'ios', label: 'iOS' },
  { value: 'android', label: 'Android' },
  { value: 'mini_program', label: '小程序' },
  { value: 'server', label: '服务端' },
] as const satisfies readonly EnumOption<Platform>[]

export const propertyTypeOptions = [
  { value: 'STRING', label: '字符串' },
  { value: 'INTEGER', label: '整数' },
  { value: 'DECIMAL', label: '小数' },
  { value: 'BOOLEAN', label: '布尔值' },
  { value: 'DATETIME', label: '日期时间' },
  { value: 'ENUM', label: '枚举' },
  { value: 'ARRAY', label: '数组' },
] as const satisfies readonly EnumOption<PropertyType>[]

export const arrayItemTypeOptions = propertyTypeOptions.filter(
  (option): option is Extract<(typeof propertyTypeOptions)[number], { value: ArrayItemType }> =>
    option.value !== 'ENUM' && option.value !== 'ARRAY',
)

export const uvModeOptions = [
  { value: 'APPROXIMATE', label: '近似统计（HLL）' },
  { value: 'EXACT', label: '精确统计' },
] as const satisfies readonly EnumOption<UVMode>[]

export const timeBasisOptions = [
  { value: 'CLIENT', label: '客户端事件时间' },
  { value: 'SERVER', label: '服务端接收时间' },
] as const satisfies readonly EnumOption<TimeBasis>[]

export const projectRoleOptions = [
  { value: 'ADMIN', label: '管理员' },
  { value: 'VIEWER', label: '只读用户' },
] as const satisfies readonly EnumOption<ProjectRole>[]

export const timezoneOptions = [
  { value: 'Asia/Shanghai', label: '上海时区（UTC+8）' },
  { value: 'UTC', label: '协调世界时（UTC）' },
] as const satisfies readonly EnumOption<SupportedTimezone>[]
