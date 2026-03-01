package service

import (
	"github.com/legamerdc/knowledge-hub/internal/server/handlers"
)

// StubService embeds the generated Unimplemented struct,
// returning 501 Not Implemented for all endpoints.
// This will be replaced with real implementations in later stages.
type StubService struct {
	handlers.Unimplemented
}

var _ handlers.ServerInterface = (*StubService)(nil)
