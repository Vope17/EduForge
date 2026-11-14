// These tests execute the production SQL schema with Node's built-in SQLite.
// Go repository and migration integration still require `go test` in the target environment.
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { DatabaseSync } from 'node:sqlite'

const schema = readFileSync(new URL('../internal/store/schema.sql', import.meta.url), 'utf8')
function fixture() {
  const db = new DatabaseSync(':memory:')
  db.exec(schema)
  db.prepare('INSERT INTO users VALUES(?,?,?,?)').run('alice', 'Alice', 1, 'now')
  db.prepare('INSERT INTO users VALUES(?,?,?,?)').run('bob', 'Bob', 0, 'now')
  const add = (id, owner, kind, { parent = null, course = null, lesson = null, data = {} } = {}) => db.prepare('INSERT INTO records VALUES(?,?,?,?,?,?,?,?,?,?)').run(id, owner, kind, parent, course, lesson, 1, JSON.stringify(data), 'now', 'now')
  return { db, add }
}
test('schema is idempotent and only one built-in local user is permitted', () => {
  const { db } = fixture()
  db.exec(schema)
  assert.equal(db.prepare('SELECT count(*) AS n FROM migrations').get().n, 1)
  assert.throws(() => db.prepare('INSERT INTO users VALUES(?,?,?,?)').run('second', 'Second', 1, 'now'), /UNIQUE/)
  db.close()
})
test('cross-user parent, course and lesson links fail at the database boundary', () => {
  const { db, add } = fixture()
  add('course-a', 'alice', 'course')
  add('lesson-a', 'alice', 'lesson', { parent: 'course-a', course: 'course-a' })
  assert.throws(() => add('child-b', 'bob', 'source', { parent: 'lesson-a' }), /FOREIGN KEY/)
  assert.throws(() => add('course-b', 'bob', 'lesson', { course: 'course-a' }), /FOREIGN KEY/)
  assert.throws(() => add('lesson-b', 'bob', 'source', { lesson: 'lesson-a' }), /FOREIGN KEY/)
  db.close()
})
test('course deletion cascades to its branches and leaves personal memory intact', () => {
  const { db, add } = fixture()
  add('p', 'alice', 'course')
  add('study', 'alice', 'lesson', { parent: 'p', course: 'p' })
  add('request', 'alice', 'request', { parent: 'p', course: 'p' })
  add('task', 'alice', 'task', { parent: 'request', course: 'p', lesson: 'study' })
  add('artifact', 'alice', 'artifact', { parent: 'task', course: 'p', lesson: 'study' })
  add('read', 'alice', 'preference')
  add('style', 'alice', 'profile')
  db.prepare('DELETE FROM records WHERE id=? AND owner_user_id=?').run('p', 'alice')
  assert.deepEqual(db.prepare('SELECT id FROM records ORDER BY id').all().map(x => x.id), ['read', 'style'])
  db.close()
})
test('completed subtask checkpoints cannot be inserted twice', () => {
  const { db, add } = fixture()
  add('task', 'alice', 'task')
  add('first', 'alice', 'step', { parent: 'task', data: { key: 'section:0' } })
  assert.throws(() => add('duplicate', 'alice', 'step', { parent: 'task', data: { key: 'section:0' } }), /UNIQUE/)
  add('second', 'alice', 'step', { parent: 'task', data: { key: 'section:1' } })
  assert.equal(db.prepare("SELECT count(*) AS n FROM records WHERE kind='step'").get().n, 2)
  db.close()
})
test('invalid JSON and unknown record kinds are rejected', () => {
  const { db, add } = fixture()
  assert.throws(() => add('bad-kind', 'alice', 'secret'), /CHECK/)
  assert.throws(() => db.prepare('INSERT INTO records VALUES(?,?,?,?,?,?,?,?,?,?)').run('bad-json', 'alice', 'event', null, null, null, 1, '{bad', 'now', 'now'), /CHECK/)
  db.close()
})
