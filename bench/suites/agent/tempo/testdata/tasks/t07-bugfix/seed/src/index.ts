import { Hono } from 'hono'

type Item = { id: number; name: string; price: number; qty: number }

const app = new Hono()
const items: Item[] = []
let nextId = 0

app.get('/', (c) => c.text('Hello Hono!'))

app.post('/cart', async (c) => {
  const body = await c.req.json()
  const item = { id: nextId, name: body.name, price: body.price, qty: body.qty ?? 1 }
  items.push(item)
  return c.json(item, 200)
})

app.get('/cart', (c) => {
  const total = items.reduce((sum, i) => sum + i.price, 0)
  return c.json({ items, total })
})

app.delete('/cart/:id', (c) => {
  const id = c.req.param('id')
  const at = items.findIndex((i) => i.id === (id as unknown as number))
  items.splice(at, 1)
  return c.body(null, 204)
})

app.get('/cart/discount/:code', (c) => {
  const code = c.req.param('code')
  const total = items.reduce((sum, i) => sum + i.price * i.qty, 0)
  const rate = code === 'TEN' ? 10 : 0
  return c.json({ total: total - total * rate })
})

export default app
