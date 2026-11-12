import { createHash } from 'node:crypto'
import { mkdir, writeFile } from 'node:fs/promises'

const fonts = [
  {
    family: 'NotoSansTC',
    directory: 'notosanstc',
    revision: '3be1884c48c3e45b52ecc725676a08f87776373e',
    sha256: '864727d210d54f2537bbe23b3a839436c3992af72de9322af5270897246bd44f',
  },
  {
    family: 'NotoSerifTC',
    directory: 'notoseriftc',
    revision: '8b0a1d0f5983c89bc2b93f1b5fb55f9e252744b5',
    sha256: '0077e18f57c6908f4a000969880940bdb0dad057c0e8d98b49dc364c3d1b09c6',
  },
]

await mkdir('public/fonts', { recursive: true })
await Promise.all(fonts.map(async (font) => {
  const base = 'https://raw.githubusercontent.com/google/fonts/' + font.revision + '/ofl/' + font.directory
  const response = await fetch(base + '/' + font.family + '%5Bwght%5D.ttf', { signal: AbortSignal.timeout(120_000) })
  if (!response.ok) throw new Error('Font download failed: ' + font.family)
  const data = Buffer.from(await response.arrayBuffer())
  if (createHash('sha256').update(data).digest('hex') !== font.sha256) {
    throw new Error('Font checksum mismatch: ' + font.family)
  }
  const license = await fetch(base + '/OFL.txt', { signal: AbortSignal.timeout(30_000) })
  if (!license.ok) throw new Error('Font license download failed: ' + font.family)
  await writeFile('public/fonts/' + font.family + '.ttf', data)
  await writeFile('public/fonts/' + font.family + '-OFL.txt', await license.text())
}))
