package mpb

import "container/heap"

var _ heap.Interface = (*priorityQueue)(nil)

type priorityQueue []*Bar

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	// greater priority pops first
	return pq[i].priority > pq[j].priority
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *priorityQueue) Push(x interface{}) {
	s := *pq
	b := x.(*Bar)
	b.index = len(s)
	*pq = append(s, b)
}

func (pq *priorityQueue) Pop() interface{} {
	var b *Bar
	s := *pq
	i := len(s) - 1
	b, s[i] = s[i], nil // nil to avoid memory leak
	b.index = -1        // for safety
	*pq = s[:i]
	return b
}
