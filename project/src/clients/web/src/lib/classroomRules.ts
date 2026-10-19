export const classroomClasses = ['hand-raising', 'reading', 'writing'] as const

export type ClassroomRule = {
  enabled: boolean
  confidence_threshold: number
  minimum_duration_ms: number
  cooldown_ms: number
  severity: 'low' | 'medium' | 'high' | 'critical'
}

export type ClassroomRules = Record<(typeof classroomClasses)[number], ClassroomRule>

export function createDefaultClassroomRules(): ClassroomRules {
  return Object.fromEntries(classroomClasses.map((name) => [name, {
    enabled: false,
    confidence_threshold: 0.6,
    minimum_duration_ms: 1000,
    cooldown_ms: 30000,
    severity: 'high',
  }])) as ClassroomRules
}

export function validateClassroomRules(rules: ClassroomRules): string | null {
  for (const rule of Object.values(rules)) {
    if (!rule.enabled) continue
    if (!Number.isFinite(rule.confidence_threshold) || rule.confidence_threshold < 0 || rule.confidence_threshold > 1
      || !Number.isFinite(rule.minimum_duration_ms) || rule.minimum_duration_ms <= 0
      || !Number.isFinite(rule.cooldown_ms) || rule.cooldown_ms < 0
      || !['low', 'medium', 'high', 'critical'].includes(rule.severity)) {
      return '已启用规则需填写 0–1 置信度、正持续时长、非负冷却时长和有效等级。'
    }
  }
  return null
}
