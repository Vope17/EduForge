import assert from 'node:assert/strict'
import { writeFile } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
const { chromium } = await import(pathToFileURL(process.env.EDUFORGE_PLAYWRIGHT_MODULE))
const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1280, height: 1000 } })
let originalLanguage = "zh-TW"
const errors = []
page.on('pageerror', error => errors.push(error.message))
try {
 await page.goto('http://127.0.0.1:8082', {waitUntil:'networkidle'})
 originalLanguage = await page.locator('html').getAttribute('lang')
 await page.getByRole('button',{name:'EN',exact:true}).click()
 await page.getByRole('button',{name:'Models & connections',exact:true}).click()
 const signIn=page.getByRole('button',{name:'Sign in with OpenAI',exact:true})
 await signIn.click()
 await page.locator('.login-code code').waitFor({timeout:45000})
 assert.ok((await page.locator('.login-code code').textContent()).trim())
 const url=await page.locator('.login-code a').getAttribute('href')
 assert.equal(new URL(url).hostname,'auth.openai.com')
 const apiForm=page.locator('.provider-form').filter({has:page.getByRole('heading',{name:'OpenAI API',exact:true})})
 let selected=''
 await page.route('**/api/providers/*',async route=>{
  if(route.request().method()!=='PUT'){return route.continue()}
  const data=route.request().postDataJSON()
  selected=data.provider.model
  await route.fulfill({json:{id:route.request().url().split('/').at(-1),data:{...data.provider,has_credential:true}}})
 })
 await page.route('**/api/providers/*/models',route=>route.fulfill({json:[{id:'fixture-model-one',name:'First available model'},{id:'fixture-model-two',name:'Second available model'}]}))
 await apiForm.locator('input[type=password]').fill('fixture-key-not-sent-to-server')
 await apiForm.getByRole('button',{name:'Save and connect',exact:true}).click()
 const select=apiForm.locator('select')
 await select.selectOption('fixture-model-two')
 await page.waitForTimeout(300)
 assert.equal(selected,'fixture-model-two')
 assert.equal(await page.getByText('Maximum output tokens',{exact:true}).count(),0)
 assert.equal(await page.getByText('Input price / million tokens (USD)',{exact:true}).count(),0)
 assert.deepEqual(errors,[])
 await page.screenshot({path:'docs/evidence/connections.png',mask:[page.locator('.login-code code')]})
 const result={officialDeviceCode:'generated',verificationHost:new URL(url).hostname,apiModelSelection:'saved after loading catalog (mock provider)',removedControls:['output token cap','input price','output price'],pageErrors:errors,subscriptionInference:'requires user to complete official sign-in'}
 await writeFile('docs/evidence/connections.json',JSON.stringify(result,null,2)+'\n')
 console.log(JSON.stringify(result))
} finally {
 await page.getByRole("button",{name:originalLanguage === "en" ? "EN" : "繁中",exact:true}).click().catch(()=>{})
 await browser.close()
}
