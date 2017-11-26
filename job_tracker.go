package sqsd

import (
	"sort"
	"sync"
)

type JobTracker struct {
	CurrentWorkings *sync.Map
	JobWorking      bool
	jobChan         chan *Job
	jobStack        chan struct{}
}

func NewJobTracker(maxProcCount uint) *JobTracker {
	procCount := int(maxProcCount)
	return &JobTracker{
		CurrentWorkings: new(sync.Map),
		JobWorking:      true,
		jobChan:         make(chan *Job, procCount),
		jobStack:        make(chan struct{}, procCount),
	}
}

func (t *JobTracker) Register(job *Job) {
	t.jobStack <- struct{}{} // blocking
	t.CurrentWorkings.Store(job.ID(), job)
	t.jobChan <- job
}

func (t *JobTracker) Complete(job *Job) {
	t.CurrentWorkings.Delete(job.ID())
	<-t.jobStack // unblock
}

func (t *JobTracker) CurrentSummaries() []*JobSummary {
	currentList := []*JobSummary{}
	t.CurrentWorkings.Range(func(key, val interface{}) bool {
		currentList = append(currentList, (val.(*Job)).Summary())
		return true
	})
	sort.Slice(currentList, func(i, j int) bool {
		return currentList[i].ReceivedAt < currentList[j].ReceivedAt
	})
	return currentList
}

func (t *JobTracker) NextJob() <-chan *Job {
	return t.jobChan
}

func (t *JobTracker) Pause() {
	t.JobWorking = false
}

func (t *JobTracker) Resume() {
	t.JobWorking = true
}

func (t *JobTracker) IsWorking() bool {
	return t.JobWorking
}
