export type Note = {
  id: string
  title: string
  body: string
  expiresAt: string | null
  createdAt: string
  updatedAt: string
}

export type CreateNoteInput = {
  title: string
  body: string
  expiresAt?: string | null
}

export type UpdateNoteInput = {
  title?: string
  body?: string
  expiresAt?: string | null
}

export function hasExpired(note: Note, at: number): boolean {
  return note.expiresAt !== null && Date.parse(note.expiresAt) <= at
}

export class NoteStore {
  private notes = new Map<string, Note>()
  private issued = 0

  create(input: CreateNoteInput): Note {
    this.issued += 1
    const now = new Date().toISOString()
    const note: Note = {
      id: `note_${this.issued}`,
      title: input.title,
      body: input.body,
      expiresAt: input.expiresAt ?? null,
      createdAt: now,
      updatedAt: now,
    }
    this.notes.set(note.id, note)
    return note
  }

  list(expired: boolean, at: number = Date.now()): Note[] {
    const all = [...this.notes.values()]
    if (!expired) return all.filter((note) => !hasExpired(note, at)).reverse()
    return all
      .filter((note) => hasExpired(note, at))
      .sort((a, b) => Date.parse(b.expiresAt as string) - Date.parse(a.expiresAt as string))
  }

  get(id: string, at: number = Date.now()): Note | undefined {
    const note = this.notes.get(id)
    if (note === undefined || hasExpired(note, at)) return undefined
    return note
  }

  update(id: string, input: UpdateNoteInput, at: number = Date.now()): Note | undefined {
    const note = this.get(id, at)
    if (note === undefined) return undefined
    const updated: Note = {
      ...note,
      title: input.title ?? note.title,
      body: input.body ?? note.body,
      expiresAt: 'expiresAt' in input ? (input.expiresAt ?? null) : note.expiresAt,
      updatedAt: new Date().toISOString(),
    }
    this.notes.set(id, updated)
    return updated
  }

  remove(id: string): boolean {
    return this.notes.delete(id)
  }
}
