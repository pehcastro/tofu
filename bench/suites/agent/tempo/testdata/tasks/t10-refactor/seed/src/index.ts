import { Hono } from 'hono'

const app = new Hono()

type User = { id: number; name: string; email: string }
type Post = { id: number; userId: number; title: string; body: string }
type Comment = { id: number; postId: number; text: string }

const users: User[] = []
const posts: Post[] = []
const comments: Comment[] = []
let userSeq = 1
let postSeq = 1
let commentSeq = 1

app.get('/', (c) => c.text('Hello Hono!'))

app.get('/users', (c) => c.json(users))

app.post('/users', async (c) => {
  const body = await c.req.json().catch(() => null)
  if (!body || typeof body.name !== 'string' || body.name.trim() === '') {
    return c.json({ error: 'name is required' }, 400)
  }
  if (typeof body.email !== 'string' || !body.email.includes('@')) {
    return c.json({ error: 'email is invalid' }, 400)
  }
  const user = { id: userSeq++, name: body.name, email: body.email }
  users.push(user)
  return c.json(user, 201)
})

app.get('/users/:id', (c) => {
  const id = Number(c.req.param('id'))
  if (!Number.isInteger(id) || id <= 0) {
    return c.json({ error: 'bad id' }, 400)
  }
  const user = users.find((u) => u.id === id)
  if (!user) {
    return c.json({ error: 'user not found' }, 404)
  }
  return c.json(user)
})

app.delete('/users/:id', (c) => {
  const id = Number(c.req.param('id'))
  if (!Number.isInteger(id) || id <= 0) {
    return c.json({ error: 'bad id' }, 400)
  }
  const at = users.findIndex((u) => u.id === id)
  if (at < 0) {
    return c.json({ error: 'user not found' }, 404)
  }
  users.splice(at, 1)
  return c.body(null, 204)
})

app.get('/posts', (c) => {
  const userId = c.req.query('userId')
  if (userId) {
    return c.json(posts.filter((p) => p.userId === Number(userId)))
  }
  return c.json(posts)
})

app.post('/posts', async (c) => {
  const body = await c.req.json().catch(() => null)
  if (!body || typeof body.title !== 'string' || body.title.trim() === '') {
    return c.json({ error: 'title is required' }, 400)
  }
  if (typeof body.body !== 'string') {
    return c.json({ error: 'body is required' }, 400)
  }
  if (!users.find((u) => u.id === body.userId)) {
    return c.json({ error: 'user not found' }, 404)
  }
  const post = { id: postSeq++, userId: body.userId, title: body.title, body: body.body }
  posts.push(post)
  return c.json(post, 201)
})

app.get('/posts/:id', (c) => {
  const id = Number(c.req.param('id'))
  if (!Number.isInteger(id) || id <= 0) {
    return c.json({ error: 'bad id' }, 400)
  }
  const post = posts.find((p) => p.id === id)
  if (!post) {
    return c.json({ error: 'post not found' }, 404)
  }
  return c.json({ ...post, comments: comments.filter((x) => x.postId === id) })
})

app.delete('/posts/:id', (c) => {
  const id = Number(c.req.param('id'))
  if (!Number.isInteger(id) || id <= 0) {
    return c.json({ error: 'bad id' }, 400)
  }
  const at = posts.findIndex((p) => p.id === id)
  if (at < 0) {
    return c.json({ error: 'post not found' }, 404)
  }
  posts.splice(at, 1)
  return c.body(null, 204)
})

app.post('/posts/:id/comments', async (c) => {
  const id = Number(c.req.param('id'))
  if (!Number.isInteger(id) || id <= 0) {
    return c.json({ error: 'bad id' }, 400)
  }
  if (!posts.find((p) => p.id === id)) {
    return c.json({ error: 'post not found' }, 404)
  }
  const body = await c.req.json().catch(() => null)
  if (!body || typeof body.text !== 'string' || body.text.trim() === '') {
    return c.json({ error: 'text is required' }, 400)
  }
  const comment = { id: commentSeq++, postId: id, text: body.text }
  comments.push(comment)
  return c.json(comment, 201)
})

app.delete('/comments/:id', (c) => {
  const id = Number(c.req.param('id'))
  if (!Number.isInteger(id) || id <= 0) {
    return c.json({ error: 'bad id' }, 400)
  }
  const at = comments.findIndex((x) => x.id === id)
  if (at < 0) {
    return c.json({ error: 'comment not found' }, 404)
  }
  comments.splice(at, 1)
  return c.body(null, 204)
})

export default app
