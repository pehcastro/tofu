package turn

import (
	"os"
	"path/filepath"
	"testing"
)

const recordedFixture = `import { describe, it, expect } from 'bun:test'
import { Hono } from 'hono'
import { app } from './app'

describe('POST /tasks', () => {
  it('accepts an explicit done flag', async () => {
    const res = await postJson('/tasks', { title: 'already done', done: true })
    expect(res.status).toBe(201)
  })

  it('rejects an over-long title with 400', async () => {
    const res = await postJson('/tasks', { title: 'x'.repeat(201) })
    expect(res.status).toBe(400)
  })
})
`

func seedRecordedFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("seeding the fixture directory: %v", err)
	}
	for _, name := range []string{"app.ts", "app.test.ts"} {
		if err := os.WriteFile(filepath.Join(root, "src", name), []byte(recordedFixture), 0o644); err != nil {
			t.Fatalf("seeding the fixture: %v", err)
		}
	}
	return root
}
