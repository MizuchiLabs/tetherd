package agent

import (
	"sync"
)

type StateManager struct {
	mu     sync.RWMutex
	hostIP string
	config []byte
}

func NewStateManager(hostIP string) *StateManager {
	return &StateManager{
		hostIP: hostIP,
		config: []byte("{}"),
	}
}

func (s *StateManager) GetConfig() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

func (s *StateManager) GetHostIP() string {
	return s.hostIP
}

func (s *StateManager) UpdateConfig(newConfig []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = newConfig
}
