import { SuffixParams, ParseLabels } from '../ProfileSource'
import { Label } from '@parca/client'

test('prefixes keys', () => {
  const input = { key: 'value' }
  expect(SuffixParams(input, '_a')).toMatchObject({ key_a: 'value' })
})

test('parses labels', () => {
  const input = ['key=value']
  expect(ParseLabels(input)).toMatchObject([{ name: 'key', value: 'value' }])
})
