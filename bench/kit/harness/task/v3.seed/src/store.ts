export type Note = {
  id: string
  title: string
  body: string
  createdAt: string
  updatedAt: string
}

export type CreateNoteInput = {
  title: string
  body: string
}

export type UpdateNoteInput = {
  title?: string
  body?: string
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
      createdAt: now,
      updatedAt: now,
    }
    this.notes.set(note.id, note)
    return note
  }

  list(): Note[] {
    return [...this.notes.values()].reverse()
  }

  get(id: string): Note | undefined {
    return this.notes.get(id)
  }

  update(id: string, input: UpdateNoteInput): Note | undefined {
    const note = this.notes.get(id)
    if (note === undefined) return undefined
    const updated: Note = {
      ...note,
      title: input.title ?? note.title,
      body: input.body ?? note.body,
      updatedAt: new Date().toISOString(),
    }
    this.notes.set(id, updated)
    return updated
  }

  remove(id: string): boolean {
    return this.notes.delete(id)
  }
}
