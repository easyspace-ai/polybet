package logsvc

import (
	"sync"
	"time"
)

type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type Service struct {
	mu      sync.RWMutex
	logs    []LogEntry
	maxSize int
}

func New() *Service {
	return &Service{
		logs:    make([]LogEntry, 0),
		maxSize: 1000,
	}
}

func (s *Service) Add(level, message string) {
	entry := LogEntry{
		Time:    time.Now().Format("2006-01-02 15:04:05"),
		Level:   level,
		Message: message,
	}
	s.mu.Lock()
	s.logs = append(s.logs, entry)
	if len(s.logs) > s.maxSize {
		s.logs = s.logs[len(s.logs)-s.maxSize:]
	}
	s.mu.Unlock()
}

func (s *Service) GetAll() []LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]LogEntry, len(s.logs))
	copy(result, s.logs)
	return result
}

func (s *Service) GetErrors() []LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var errors []LogEntry
	for _, l := range s.logs {
		if l.Level == "error" || l.Level == "warn" {
			errors = append(errors, l)
		}
	}
	return errors
}

func (s *Service) Clear() {
	s.mu.Lock()
	s.logs = s.logs[:0]
	s.mu.Unlock()
}