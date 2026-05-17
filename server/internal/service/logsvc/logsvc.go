package logsvc

import (
	"sync"
	"time"
)

type LogEntry struct {
	timestamp time.Time
	Time      string `json:"time"`
	Level     string `json:"level"`
	Category  string `json:"category"`
	Message   string `json:"message"`
}

type Service struct {
	mu       sync.RWMutex
	logs     []LogEntry
	maxAge   time.Duration
	interval time.Duration
	ticker   *time.Ticker
	stopCh   chan struct{}
}

func New() *Service {
	svc := &Service{
		logs:     make([]LogEntry, 0),
		maxAge:   3 * 24 * time.Hour,
		interval: time.Hour,
		stopCh:   make(chan struct{}),
	}
	go svc.cleanupLoop()
	return svc
}

func (s *Service) cleanupLoop() {
	s.ticker = time.NewTicker(s.interval)
	defer s.ticker.Stop()
	for {
		select {
		case <-s.ticker.C:
			s.cleanup()
		case <-s.stopCh:
			return
		}
	}
}

func (s *Service) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-s.maxAge)
	i := 0
	for ; i < len(s.logs); i++ {
		if s.logs[i].timestamp.After(cutoff) {
			break
		}
	}
	if i > 0 {
		s.logs = s.logs[i:]
	}
}

func (s *Service) Add(level, category, message string) {
	entry := LogEntry{
		timestamp: time.Now(),
		Time:      time.Now().Format("2006-01-02 15:04:05"),
		Level:     level,
		Category:  category,
		Message:   message,
	}
	s.mu.Lock()
	s.logs = append(s.logs, entry)
	if len(s.logs) > 10000 {
		s.logs = s.logs[len(s.logs)-10000:]
	}
	s.mu.Unlock()
}

func (s *Service) Info(category, message string) {
	s.Add("info", category, message)
}

func (s *Service) Warn(category, message string) {
	s.Add("warn", category, message)
}

func (s *Service) Error(category, message string) {
	s.Add("error", category, message)
}

func (s *Service) GetAll() []LogEntry {
	s.mu.RLock()
	result := make([]LogEntry, len(s.logs))
	copy(result, s.logs)
	s.mu.RUnlock()
	return result
}

func (s *Service) GetErrors() []LogEntry {
	s.mu.RLock()
	var errors []LogEntry
	for _, l := range s.logs {
		if l.Level == "error" || l.Level == "warn" {
			errors = append(errors, l)
		}
	}
	s.mu.RUnlock()
	return errors
}

func (s *Service) Clear() {
	s.mu.Lock()
	s.logs = s.logs[:0]
	s.mu.Unlock()
}

func (s *Service) Stop() {
	close(s.stopCh)
}
