// Writes apps/frontend/public/third-party-licenses.txt: the licences of the libraries that the page and the server
// carry, because their licences require that the notices travel with the files. It reads the Go modules that the two programs of the backend compile
// (`go list -deps`) and the packages of apps/frontend/package-lock.json that are not development tools.
//
//   node scripts/licenses.mjs
//
// Run it after `go mod tidy` or `npm install`, with the modules downloaded and apps/frontend/node_modules installed.
import { execFileSync } from 'node:child_process'
import { existsSync, readdirSync, readFileSync, writeFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const backend = join(root, 'apps/backend')
const frontend = join(root, 'apps/frontend')
const out = join(frontend, 'public/third-party-licenses.txt')

const licenceFile = (dir) => {
  const name = readdirSync(dir).find((f) => /^(licen[cs]e|copying|unlicense)(\.(md|txt))?$/i.test(f))
  return name ? readFileSync(join(dir, name), 'utf8').trim() : null
}

// The SPDX name of a licence text, from words that only that licence has.
const spdx = (text) => {
  if (/Apache License/.test(text) && /Version 2\.0/.test(text)) return 'Apache-2.0'
  if (/Permission to use, copy, modify, and\/or distribute/.test(text)) return 'ISC'
  if (/Mozilla Public License Version 2\.0/.test(text)) return 'MPL-2.0'
  if (/Neither the name of/.test(text)) return 'BSD-3-Clause'
  if (/Redistributions of source code must retain/.test(text)) return 'BSD-2-Clause'
  if (/Permission is hereby granted, free of charge/.test(text)) return 'MIT'
  return 'see text'
}

const packages = []

// Only the modules that the two programs compile, not every module in the dependency graph.
const goModules = execFileSync(
  'go',
  ['list', '-deps', '-f', '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}', './cmd/...'],
  { cwd: backend, encoding: 'utf8', maxBuffer: 1 << 26 },
)
  .split('\n')
  .filter(Boolean)
  .map((line) => line.split(' '))
for (const [path, version, dir] of [...new Map(goModules.map((m) => [m[0], m])).values()]) {
  const text = licenceFile(dir)
  if (text) packages.push({ name: path, version, kind: 'Go module', text })
}

const lock = JSON.parse(readFileSync(join(frontend, 'package-lock.json'), 'utf8'))
for (const [path, entry] of Object.entries(lock.packages)) {
  if (!path || entry.dev) continue
  // Types are not part of the bundle.
  if (/node_modules\/(@types\/|csstype$)/.test(path)) continue
  const dir = join(frontend, path)
  const text = existsSync(dir) ? licenceFile(dir) : null
  const name = path.replace(/^.*node_modules\//, '')
  packages.push({ name, version: entry.version, kind: 'npm package', text: text ?? `License: ${entry.license}` })
}

packages.sort((a, b) => a.name.localeCompare(b.name))

const groups = new Map()
for (const p of packages) {
  const key = p.text
  if (!groups.has(key)) groups.set(key, [])
  groups.get(key).push(p)
}

const goRuntime = readFileSync(join(execFileSync('go', ['env', 'GOROOT'], { encoding: 'utf8' }).trim(), 'LICENSE'), 'utf8').trim()

const lines = [
  'THIRD-PARTY NOTICES',
  '',
  'This project (MIT License, see LICENSE in the source repository) is built from the libraries listed below. Their',
  'licences require that these notices travel with the files.',
  '',
  'The Docker images also contain separate programs under their own licences: ffmpeg (LGPL or GPL, depending on the',
  'build of the distribution), espeak-ng (GPL-3.0, only in the image of the AI presenter), coturn (BSD-3-Clause) and',
  'Ollama (MIT). The project runs them as programs and does not link them.',
  '',
  'LIBRARIES',
  '',
  `  * Go standard library and runtime (${spdx(goRuntime)})`,
  ...packages.map((p) => `  * ${p.name} ${p.version} (${p.kind}, ${spdx(p.text)})`),
  '',
]
lines.push('='.repeat(78), 'Go standard library and runtime', '='.repeat(78), '', goRuntime, '')
for (const [text, list] of groups) {
  lines.push('='.repeat(78), list.map((p) => `${p.name} ${p.version}`).join(', '), '='.repeat(78), '', text, '')
}
writeFileSync(out, lines.join('\n'))
console.log(`wrote ${out}: ${packages.length} libraries, ${groups.size} distinct texts`)
