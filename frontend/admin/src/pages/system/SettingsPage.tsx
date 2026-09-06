import { InfoCircleOutlined } from '@ant-design/icons'
import { Alert, App, Button, Input, InputNumber, Select, Switch, Tooltip } from 'antd'
import { useState } from 'react'
import { AsyncBoundary } from '../../components/common/AsyncBoundary'
import { PageHeader } from '../../components/common/PageHeader'
import { useAsyncData } from '../../hooks/useAsyncData'
import { systemApi } from '../../api'

/**
 * 平台设置。
 *
 * 与用户端设置的关键差别：这里每一项都影响全平台。所以采用「显式保存」而非
 * 即时生效 —— 拨错一个开关就让所有用户的数据承诺变样，代价太大。
 */
export default function SettingsPage() {
  const { message } = App.useApp()
  const { data, loading, error, reload } = useAsyncData(() => systemApi.settings(), [])
  const [dirty, setDirty] = useState(false)

  return (
    <div className="pb-16">
      <PageHeader description="平台级默认配置。每一项都影响全部用户，修改需显式保存。" />

      <AsyncBoundary loading={loading} error={error} onRetry={reload} skeletonRows={10}>
        {data && (
          <div className="flex flex-col">
            {data.groups.map((group) => (
              <section
                key={group.key}
                className="grid gap-x-12 gap-y-4 border-b border-line py-7 first:pt-0 lg:grid-cols-[minmax(190px,0.35fr)_minmax(0,0.65fr)]"
              >
                <div>
                  <h2 className="m-0 text-sm font-semibold">{group.title}</h2>
                  <p className="mt-1.5 mb-0 text-[10px] leading-relaxed text-ink-muted">
                    {group.description}
                  </p>
                </div>

                <div className="flex flex-col gap-3">
                  {group.items.map((item) => (
                    <div
                      key={item.key}
                      className="flex items-center justify-between gap-5 rounded border border-line bg-surface px-3.5 py-3"
                    >
                      <span className="min-w-0">
                        <b className="flex items-center gap-1.5 text-[11px]">
                          {item.label}
                          <Tooltip title={item.hint}>
                            <InfoCircleOutlined className="cursor-help text-[10px] text-ink-muted" />
                          </Tooltip>
                        </b>
                        <small className="mt-0.5 block text-[10px] text-ink-muted">
                          {item.hint}
                        </small>
                      </span>

                      <span className="shrink-0" onChange={() => setDirty(true)}>
                        {item.type === 'switch' && (
                          <Switch
                            defaultChecked={Boolean(item.value)}
                            onChange={() => setDirty(true)}
                          />
                        )}
                        {item.type === 'number' && (
                          <InputNumber
                            size="small"
                            defaultValue={Number(item.value)}
                            addonAfter={item.unit}
                            className="w-32"
                            onChange={() => setDirty(true)}
                          />
                        )}
                        {item.type === 'select' && (
                          <Select
                            size="small"
                            defaultValue={String(item.value)}
                            className="w-40"
                            onChange={() => setDirty(true)}
                            options={(item.options ?? []).map((v) => ({ value: v, label: v }))}
                          />
                        )}
                        {item.type === 'text' && (
                          <Input
                            size="small"
                            defaultValue={String(item.value)}
                            className="w-40"
                            onChange={() => setDirty(true)}
                          />
                        )}
                      </span>
                    </div>
                  ))}
                </div>
              </section>
            ))}
          </div>
        )}
      </AsyncBoundary>

      {/* 显式保存条。停留在底部，让「有未保存修改」这件事无法被忽略 */}
      <div className="sticky bottom-0 -mx-6 mt-6 flex items-center justify-end gap-3 border-t border-line bg-plane/95 px-6 py-3 backdrop-blur lg:-mx-10 lg:px-10">
        {dirty ? (
          <Alert
            type="warning"
            showIcon
            className="mr-auto py-1"
            message={<span className="text-[10px]">有未保存的修改，这些配置将影响全部用户</span>}
          />
        ) : (
          <span className="mr-auto text-[10px] text-ink-muted">配置不会自动保存</span>
        )}
        <Button size="small" disabled={!dirty} onClick={() => { setDirty(false); reload() }}>
          放弃修改
        </Button>
        <Button
          size="small"
          type="primary"
          disabled={!dirty}
          onClick={() => {
            setDirty(false)
            message.success('平台设置已保存')
          }}
        >
          保存更改
        </Button>
      </div>
    </div>
  )
}
