export function Metric({ label, value, note }: { label: string; value: string; note: string }) {
  return <div className="metric"><small>{label}</small><b>{value}</b><span>{note}</span></div>
}

