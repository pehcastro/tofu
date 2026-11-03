export type Task = {
  id: string
  title: string
  done: boolean
  createdAt: string
  updatedAt: string
}

export type CreateTaskInput = {
  title: string
  done?: boolean
}

export type UpdateTaskInput = {
  title?: string
  done?: boolean
}

/**
 * In-memory task storage. A class (rather than module-level state) so that
 * every app instance — in particular every test — starts from a clean slate.
 */
export class TaskStore {
  #tasks = new Map<string, Task>()
  #nextId = 1

  list(): Task[] {
    return [...this.#tasks.values()]
  }

  get(id: string): Task | undefined {
    return this.#tasks.get(id)
  }

  create(input: CreateTaskInput): Task {
    const now = new Date().toISOString()
    const task: Task = {
      id: String(this.#nextId++),
      title: input.title,
      done: input.done ?? false,
      createdAt: now,
      updatedAt: now,
    }
    this.#tasks.set(task.id, task)
    return task
  }

  update(id: string, input: UpdateTaskInput): Task | undefined {
    const task = this.#tasks.get(id)
    if (!task) return undefined

    const updated: Task = {
      ...task,
      ...(input.title !== undefined ? { title: input.title } : {}),
      ...(input.done !== undefined ? { done: input.done } : {}),
      updatedAt: new Date().toISOString(),
    }
    this.#tasks.set(id, updated)
    return updated
  }

  delete(id: string): Task | undefined {
    const task = this.#tasks.get(id)
    if (!task) return undefined
    this.#tasks.delete(id)
    return task
  }
}
