// Package testutil supplies an isolated, optimistic-concurrency repository for tests.
package testutil

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

type Memory struct {
	mu         sync.Mutex
	Games      map[string]*models.Game
	Access     map[string]models.Access
	BeforeSave func(*models.Game)
}

func NewMemory() *Memory {
	return &Memory{Games: map[string]*models.Game{}, Access: map[string]models.Access{}}
}
func clone(g *models.Game) *models.Game {
	data, _ := json.Marshal(g)
	var result models.Game
	_ = json.Unmarshal(data, &result)
	return &result
}
func (m *Memory) Insert(_ context.Context, g *models.Game) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.Games {
		if existing.InvitationCode == g.InvitationCode {
			return services.ErrConflict
		}
	}
	m.Games[g.ID] = clone(g)
	return nil
}
func (m *Memory) Get(_ context.Context, id string) (*models.Game, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.Games[id]
	if !ok {
		return nil, services.ErrNotFound
	}
	return clone(g), nil
}
func (m *Memory) ByCode(_ context.Context, code string) (*models.Game, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, g := range m.Games {
		if g.InvitationCode == code {
			return clone(g), nil
		}
	}
	return nil, services.ErrNotFound
}
func (m *Memory) Save(_ context.Context, g *models.Game, previous int64) error {
	if m.BeforeSave != nil {
		m.BeforeSave(g)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.Games[g.ID]
	if !ok || existing.Revision != previous {
		return services.ErrConflict
	}
	copy := clone(g)
	copy.PublishedRevision = existing.PublishedRevision
	m.Games[g.ID] = copy
	return nil
}
func (m *Memory) PutAccess(_ context.Context, a models.Access) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Access[a.Hash] = a
	return nil
}
func (m *Memory) GetAccess(_ context.Context, hash string) (models.Access, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.Access[hash]
	if !ok {
		return a, services.ErrNotFound
	}
	return a, nil
}
