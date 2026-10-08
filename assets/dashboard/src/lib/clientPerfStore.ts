// IndexedDB persistence for the client performance recorder. One record per
// tab so two tabs recording at once never overwrite each other.
const DB_NAME = 'schmux-client-perf';
const STORE = 'snapshots';

function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, 1);
    req.onupgradeneeded = () => req.result.createObjectStore(STORE);
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

// Resolves with the request result and the main-thread time the request call
// itself took: for put() that is the structured clone, the recorder's own cost.
function tx<T>(
  mode: IDBTransactionMode,
  run: (store: IDBObjectStore) => IDBRequest<T>
): Promise<{ result: T; callMs: number }> {
  return open().then(
    (db) =>
      new Promise<{ result: T; callMs: number }>((resolve, reject) => {
        const t = db.transaction(STORE, mode);
        const t0 = performance.now();
        const req = run(t.objectStore(STORE));
        const callMs = performance.now() - t0;
        req.onsuccess = () => resolve({ result: req.result, callMs });
        req.onerror = () => reject(req.error);
        t.oncomplete = () => db.close();
      })
  );
}

/** Writes the snapshot; resolves with the milliseconds the synchronous put() took. */
export function saveSnapshot(tabId: string, value: unknown): Promise<number> {
  return tx('readwrite', (s) => s.put(value, tabId)).then((r) => r.callMs);
}

export function loadSnapshot<T>(tabId: string): Promise<T | undefined> {
  return tx<T | undefined>('readonly', (s) => s.get(tabId) as IDBRequest<T | undefined>).then(
    (r) => r.result
  );
}

export function clearSnapshot(tabId: string): Promise<void> {
  return tx('readwrite', (s) => s.delete(tabId)).then(() => undefined);
}
