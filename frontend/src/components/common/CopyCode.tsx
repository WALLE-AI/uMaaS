import { CopyOutlined } from '@ant-design/icons'
import { Button, message } from 'antd'

export function CopyCode({ code }: { code: string }) {
  const [api, contextHolder] = message.useMessage()

  const copy = async () => {
    await navigator.clipboard.writeText(code)
    api.success('已复制')
  }

  return <>{contextHolder}<Button className="copy-code" icon={<CopyOutlined />} onClick={copy}>复制</Button></>
}

