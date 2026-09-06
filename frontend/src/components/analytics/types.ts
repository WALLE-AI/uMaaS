import type { Model } from '../../data'

export type UsageDatum = {
  date: string
  openai: number
  google: number
  anthropic: number
  deepseek: number
  others: number
}

export type BenchmarkRecord = {
  name: string
  group: string
  description: string
  models: number
  quality: string
  value: string
  speed: string
  winners: string[]
}

export type RankingInsightData = {
  name: string
  title: string
  description: string
  labels: string[]
  values: string[]
}

export type RankingModel = Pick<Model, 'id' | 'name' | 'maker' | 'logo' | 'color' | 'initials'>
