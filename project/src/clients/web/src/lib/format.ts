export function formatDate(value?: string, includeDate = true) {
  if (!value) return '暂无数据'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '暂无数据'
  return new Intl.DateTimeFormat('zh-CN', includeDate
    ? { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }
    : { hour: '2-digit', minute: '2-digit' }).format(date)
}

export function severityLabel(value: string) {
  return ({ low: '低', medium: '中', high: '高', critical: '紧急' } as Record<string, string>)[value] ?? value
}

export function statusLabel(value: string) {
  return ({ pending: '待处置', processing: '处理中', resolved: '已解除', false_positive: '误报' } as Record<string, string>)[value] ?? value
}

export function behaviorLabel(value: string) {
  return ({
    'classroom.behavior.hand-raising': '举手行为',
    'classroom.behavior.reading': '阅读行为',
    'classroom.behavior.writing': '书写行为',
    'hand-raising': '举手', reading: '阅读', writing: '书写',
  } as Record<string, string>)[value] ?? value.replace(/^classroom\.behavior\./, '').replaceAll('-', ' ')
}

export function countLabel(value: number) {
  return new Intl.NumberFormat('zh-CN').format(value)
}
