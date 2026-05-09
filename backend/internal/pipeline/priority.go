package pipeline

import (
	"container/heap"
	"qiuqiu/internal/event"
)

// PriorityQueue implements a min-heap for events ordered by priority.
// It implements container/heap.Interface.
type PriorityQueue struct {
	items []*event.StandardEvent
}

func NewPriorityQueue() *PriorityQueue {
	pq := &PriorityQueue{items: make([]*event.StandardEvent, 0)}
	heap.Init(pq)
	return pq
}

// heap.Interface methods
func (pq *PriorityQueue) Len() int           { return len(pq.items) }
func (pq *PriorityQueue) Less(i, j int) bool { return pq.items[i].Priority() < pq.items[j].Priority() }
func (pq *PriorityQueue) Swap(i, j int)      { pq.items[i], pq.items[j] = pq.items[j], pq.items[i] }
func (pq *PriorityQueue) Push(x any)         { pq.items = append(pq.items, x.(*event.StandardEvent)) }
func (pq *PriorityQueue) Pop() any {
	old := pq.items
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	pq.items = old[:n-1]
	return item
}

// PushEvent adds an event to the priority queue.
func (pq *PriorityQueue) PushEvent(ev *event.StandardEvent) {
	heap.Push(pq, ev)
}

// PopEvent pops the highest priority event.
func (pq *PriorityQueue) PopEvent() *event.StandardEvent {
	if pq.Len() == 0 {
		return nil
	}
	return heap.Pop(pq).(*event.StandardEvent)
}
