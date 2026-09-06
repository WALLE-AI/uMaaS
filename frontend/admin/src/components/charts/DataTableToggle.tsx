import { TableOutlined } from '@ant-design/icons'
import { Button, Drawer, Table, Tooltip } from 'antd'
import { formatMetric, type MetricUnit } from '../../config/metrics'

/**
 * 「查看数据表」入口。
 *
 * 这不是锦上添花的功能，而是 §4.1 对比度 WARN 的缓解手段：青/黄/品红三个
 * 色槽在白色绘图面上对比度低于 3:1，靠颜色无法可靠区分系列，必须让读者能
 * 拿到确切数值。同时它也承担无障碍要求下的表格视图。
 */
export function DataTableToggle({
  open,
  onOpenChange,
  data,
  xKey,
  series,
  unit,
  reason,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  data: Array<Record<string, string | number>>
  xKey: string
  series: Array<{ id: string; label: string; color: string }>
  unit: MetricUnit
  reason: 'low-contrast' | 'explicit'
}) {
  const hint =
    reason === 'low-contrast'
      ? '本图使用了低对比度色槽，已按无障碍规则提供数据表'
      : '查看图表背后的原始数值'

  const columns = [
    { title: '', dataIndex: xKey, key: xKey, width: 110, fixed: 'left' as const },
    ...series.map((item) => ({
      title: (
        <span className="inline-flex items-center gap-1.5">
          <i
            className="inline-block h-2 w-2 shrink-0 rounded-full"
            style={{ background: item.color }}
          />
          {item.label}
        </span>
      ),
      dataIndex: item.id,
      key: item.id,
      align: 'right' as const,
      render: (value: number) =>
        typeof value === 'number' ? formatMetric(value, unit) : (value ?? '—'),
    })),
  ]

  return (
    <>
      <div className="mt-2 flex justify-end">
        <Tooltip title={hint}>
          <Button
            type="text"
            size="small"
            icon={<TableOutlined />}
            className="text-xs text-ink-muted"
            onClick={() => onOpenChange(true)}
          >
            查看数据表
          </Button>
        </Tooltip>
      </div>
      <Drawer
        title="数据表"
        width={720}
        open={open}
        onClose={() => onOpenChange(false)}
        destroyOnHidden
      >
        <p className="mb-3 text-xs text-ink-muted">{hint}</p>
        <Table
          size="small"
          bordered={false}
          pagination={false}
          scroll={{ x: true, y: '70vh' }}
          rowKey={(row) => String(row[xKey])}
          columns={columns}
          dataSource={data}
        />
      </Drawer>
    </>
  )
}
