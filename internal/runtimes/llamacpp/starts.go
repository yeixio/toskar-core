package llamacpp

import "sync"

// keyedMutex is a lock per key that's dropped once nobody holds or waits
// for it.
type keyedMutex struct {
	mu   sync.Mutex
	keys map[string]*keyedLock
}

type keyedLock struct {
	sync.Mutex
	refs int
}

// lock takes key's lock and returns what releases it.
func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	if k.keys == nil {
		k.keys = map[string]*keyedLock{}
	}
	l := k.keys[key]
	if l == nil {
		l = &keyedLock{}
		k.keys[key] = l
	}
	l.refs++
	k.mu.Unlock()
	l.Lock()
	return func() {
		l.Unlock()
		k.mu.Lock()
		if l.refs--; l.refs == 0 {
			delete(k.keys, key)
		}
		k.mu.Unlock()
	}
}
