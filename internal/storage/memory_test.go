package storage

import "testing"

func TestMemoryStoreCompliance(t *testing.T) {
	RunCompliance(t, "MemoryStore", func() Store {
		return NewMemoryStore()
	})
}
