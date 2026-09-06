import { Link } from 'react-router'

export function Brand() {
  return (
    <Link className="brand" to="/" aria-label="uMaaS 首页">
      <span className="brand-mark"><i /><i /><i /></span>
      <b>uMaaS</b>
      <span className="brand-beta">BETA</span>
    </Link>
  )
}

