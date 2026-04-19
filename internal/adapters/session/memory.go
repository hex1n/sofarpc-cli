package session

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/ports"
)

var _ ports.SessionStore = (*MemoryStore)(nil)

type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]model.WorkspaceSession
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: map[string]model.WorkspaceSession{},
	}
}

func (s *MemoryStore) Save(_ context.Context, session model.WorkspaceSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = cloneSession(session)
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (model.WorkspaceSession, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	if !ok {
		return model.WorkspaceSession{}, false, nil
	}
	return cloneSession(session), true, nil
}

func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return nil
}

func (s *MemoryStore) List(_ context.Context) ([]model.WorkspaceSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessions := make([]model.WorkspaceSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, cloneSession(session))
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].UpdatedAt == sessions[j].UpdatedAt {
			return sessions[i].ID < sessions[j].ID
		}
		return sessions[i].UpdatedAt > sessions[j].UpdatedAt
	})
	return sessions, nil
}

func cloneSession(session model.WorkspaceSession) model.WorkspaceSession {
	session.Notes = append([]string{}, session.Notes...)
	if session.LastTarget != nil {
		target := *session.LastTarget
		session.LastTarget = &target
	}
	if session.LastDescribe != nil {
		describe := *session.LastDescribe
		describe.Overloads = cloneMethodOverloads(describe.Overloads)
		if describe.Selected != nil {
			selected := *describe.Selected
			selected.ParamTypes = append([]string{}, selected.ParamTypes...)
			selected.ParamTypeSignatures = append([]string{}, selected.ParamTypeSignatures...)
			describe.Selected = &selected
		}
		describe.Diagnostics.ContractNotes = append([]string{}, describe.Diagnostics.ContractNotes...)
		session.LastDescribe = &describe
	}
	if session.LastPlan != nil {
		plan := *session.LastPlan
		plan.Request.ParamTypes = append([]string{}, plan.Request.ParamTypes...)
		plan.Request.ParamTypeSignatures = append([]string{}, plan.Request.ParamTypeSignatures...)
		plan.Request.Args = cloneRawMessage(plan.Request.Args)
		plan.Spec.StubPaths = append([]string{}, plan.Spec.StubPaths...)
		plan.Runtime.ContractNotes = append([]string{}, plan.Runtime.ContractNotes...)
		session.LastPlan = &plan
	}
	return session
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	return append(json.RawMessage{}, raw...)
}

func cloneMethodOverloads(overloads []model.WorkspaceMethodOverload) []model.WorkspaceMethodOverload {
	cloned := make([]model.WorkspaceMethodOverload, 0, len(overloads))
	for _, overload := range overloads {
		item := overload
		item.ParamTypes = append([]string{}, item.ParamTypes...)
		item.ParamTypeSignatures = append([]string{}, item.ParamTypeSignatures...)
		cloned = append(cloned, item)
	}
	return cloned
}
