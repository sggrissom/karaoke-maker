package backend

import "sync"

var (
	jobSubs   = map[string][]chan Job{}
	jobSubsMu sync.Mutex
)

func Subscribe(jobID string) (<-chan Job, func()) {
	ch := make(chan Job, 8)
	jobSubsMu.Lock()
	jobSubs[jobID] = append(jobSubs[jobID], ch)
	jobSubsMu.Unlock()

	return ch, func() {
		jobSubsMu.Lock()
		subs := jobSubs[jobID]
		for i, s := range subs {
			if s == ch {
				jobSubs[jobID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		if len(jobSubs[jobID]) == 0 {
			delete(jobSubs, jobID)
		}
		jobSubsMu.Unlock()
		close(ch)
	}
}

func notifySubscribers(jobID string, job Job) {
	jobSubsMu.Lock()
	defer jobSubsMu.Unlock()
	for _, ch := range jobSubs[jobID] {
		select {
		case ch <- job:
		default:
		}
	}
}
