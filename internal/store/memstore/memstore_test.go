package memstore_test

import (
	"testing"

	"github.com/example/workspace-api/internal/store"
	"github.com/example/workspace-api/internal/store/memstore"
	"github.com/example/workspace-api/internal/store/storetest"
)

// The in-memory store must satisfy the same contract as the SQL stores, so
// the API unit tests that rely on it exercise realistic behaviour.
func TestContract(t *testing.T) {
	storetest.Run(t, func(*testing.T) store.Store { return memstore.New() })
}
