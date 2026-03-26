package plexdash

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// discoveryJobState is the lifecycle of an async discovery job.
type discoveryJobState string

const (
	jobQueued  discoveryJobState = "queued"
	jobRunning discoveryJobState = "running"
	jobDone    discoveryJobState = "done"
	jobFailed  discoveryJobState = "error"
)

// discoveryJob holds the state for one background discovery run.
type discoveryJob struct {
	ID        string
	State     discoveryJobState
	Message   string // human-readable progress / status text
	Result    any    // set when State == jobDone
	ErrMsg    string // set when State == jobFailed
	CreatedAt time.Time
}

// discoveryJobStore keeps all in-flight and recently completed discovery jobs.
type discoveryJobStore struct {
	mu   sync.Mutex
	jobs map[string]*discoveryJob
}

func newDiscoveryJobStore() *discoveryJobStore {
	return &discoveryJobStore{jobs: make(map[string]*discoveryJob)}
}

func (s *discoveryJobStore) create() *discoveryJob {
	b := make([]byte, 8)
	rand.Read(b) //nolint:errcheck
	id := hex.EncodeToString(b)

	job := &discoveryJob{
		ID:        id,
		State:     jobQueued,
		Message:   "Queued",
		CreatedAt: time.Now(),
	}

	s.mu.Lock()
	s.jobs[id] = job
	// Prune stale jobs older than 2 hours so the map never grows unbounded.
	for k, j := range s.jobs {
		if time.Since(j.CreatedAt) > 2*time.Hour {
			delete(s.jobs, k)
		}
	}
	s.mu.Unlock()
	return job
}

func (s *discoveryJobStore) get(id string) (*discoveryJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	return j, ok
}

// Transition helpers — all writes go through the store mutex.

func (s *discoveryJobStore) setRunning(job *discoveryJob, msg string) {
	s.mu.Lock()
	job.State = jobRunning
	job.Message = msg
	s.mu.Unlock()
}

func (s *discoveryJobStore) setDone(job *discoveryJob, result any) {
	s.mu.Lock()
	job.State = jobDone
	job.Result = result
	job.Message = "Done"
	s.mu.Unlock()
}

func (s *discoveryJobStore) setFailed(job *discoveryJob, errMsg string) {
	s.mu.Lock()
	job.State = jobFailed
	job.ErrMsg = errMsg
	job.Message = "Failed"
	s.mu.Unlock()
}
