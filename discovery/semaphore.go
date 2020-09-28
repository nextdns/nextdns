package discovery

import "sync"

type semaphoreMap struct {
	mu       sync.Mutex
	acquired map[string]struct{}
}

func (s *semaphoreMap) Acquire(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, found := s.acquired[key]; !found {
		if s.acquired == nil {
			s.acquired = map[string]struct{}{}
		}
		s.acquired[key] = struct{}{}
		return true
	}
	return false
}

func (s *semaphoreMap) Release(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.acquired, key)
}
