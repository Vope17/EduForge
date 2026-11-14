// Renders TeX formulas to SVG with MathJax for EduForge's exports.
// stdin: JSON [{"tex": "...", "display": bool}]
// stdout: JSON [{"svg": "...", "width": ex, "height": ex, "depth": ex, "error": "..."}]
// Sizes are in ex of the surrounding text; depth is how far the formula
// reaches below the baseline. A formula MathJax cannot parse has an error.
import { mathjax } from 'mathjax-full/js/mathjax.js'
import { TeX } from 'mathjax-full/js/input/tex.js'
import { SVG } from 'mathjax-full/js/output/svg.js'
import { liteAdaptor } from 'mathjax-full/js/adaptors/liteAdaptor.js'
import { RegisterHTMLHandler } from 'mathjax-full/js/handlers/html.js'
import { AllPackages } from 'mathjax-full/js/input/tex/AllPackages.js'

const adaptor = liteAdaptor()
RegisterHTMLHandler(adaptor)
// No packages that load files or define macros from the input.
const packages = AllPackages.filter(name => !['require', 'autoload', 'configmacros', 'newcommand', 'html'].includes(name))
// A fresh document per formula: one formula cannot redefine macros for others.
const newDocument = () => mathjax.document('', { InputJax: new TeX({ packages, formatError: (_, err) => { throw err } }), OutputJax: new SVG({ fontCache: 'none' }) })

function render({ tex = '', display = false }) {
  const doc = newDocument()
  try {
    if (typeof tex !== 'string' || !tex.trim() || tex.length > 4000) throw new Error('empty or too long')
    const svg = adaptor.firstChild(doc.convert(tex, { display: !!display }))
    const num = name => parseFloat(adaptor.getAttribute(svg, name)) || 0
    const align = (adaptor.getAttribute(svg, 'style') || '').match(/vertical-align:\s*(-?[\d.]+)ex/)
    adaptor.removeAttribute(svg, 'style')
    // Formulas are pictures: no links or scripts survive.
    const markup = adaptor.outerHTML(svg).replace(/\s(?:xlink:)?href="[^"]*"/g, '').replace(/<\/?(?:a|script|foreignObject)\b[^>]*>/gi, '')
    return { svg: markup, width: num('width'), height: num('height'), depth: align ? -parseFloat(align[1]) : 0 }
  } catch (err) {
    return { error: String(err?.message || err).slice(0, 300) }
  }
}

let input = ''
process.stdin.setEncoding('utf8')
process.stdin.on('data', chunk => { input += chunk })
process.stdin.on('end', () => {
  const items = JSON.parse(input || '[]')
  process.stdout.write(JSON.stringify(Array.isArray(items) ? items.slice(0, 2000).map(render) : []))
})
