// SPDX-License-Identifier: AGPL-3.0-or-later

import { readdir, readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { NodeTypes, parse as parseTemplate } from '@vue/compiler-dom'
import { parse as parseSfc } from '@vue/compiler-sfc'

const sourceDirectory = fileURLToPath(new URL('../src/', import.meta.url))
const criticalAttributes = new Set([
  'alt',
  'aria-description',
  'aria-label',
  'placeholder',
  'title',
])
const containsLetter = /\p{Letter}/u

async function vueFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true })
  const nested = await Promise.all(entries.map((entry) => {
    const path = `${directory}/${entry.name}`
    return entry.isDirectory() ? vueFiles(path) : (entry.name.endsWith('.vue') ? [path] : [])
  }))
  return nested.flat()
}

function location(file, node) {
  return `${file}:${node.loc.start.line}:${node.loc.start.column}`
}

function inspectNode(file, node, violations) {
  if (node.type === NodeTypes.TEXT) {
    const text = node.content.trim()
    if (containsLetter.test(text)) {
      violations.push(`${location(file, node)} literal text ${JSON.stringify(text)}`)
    }
  }
  if (node.type === NodeTypes.ELEMENT) {
    for (const property of node.props) {
      if (
        property.type === NodeTypes.ATTRIBUTE
        && criticalAttributes.has(property.name)
        && property.value
        && containsLetter.test(property.value.content)
      ) {
        violations.push(
          `${location(file, property)} literal ${property.name} ${JSON.stringify(property.value.content)}`,
        )
      }
    }
  }
  if ('children' in node && Array.isArray(node.children)) {
    for (const child of node.children) inspectNode(file, child, violations)
  }
  if (node.type === NodeTypes.IF) {
    for (const branch of node.branches) inspectNode(file, branch, violations)
  }
}

const violations = []
for (const file of await vueFiles(sourceDirectory)) {
  const source = await readFile(file, 'utf8')
  const { descriptor, errors } = parseSfc(source, { filename: file })
  if (errors.length > 0) {
    for (const error of errors) violations.push(`${file}: ${String(error)}`)
    continue
  }
  if (!descriptor.template) continue
  const template = parseTemplate(descriptor.template.content, { comments: true })
  inspectNode(file, template, violations)
}

if (violations.length > 0) {
  console.error('Critical user-interface literals must use translation keys:')
  for (const violation of violations) console.error(`- ${violation}`)
  process.exitCode = 1
} else {
  console.log('No untranslated critical Vue template literals found.')
}
