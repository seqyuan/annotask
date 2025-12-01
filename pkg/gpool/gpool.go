package gpool

import (
	"sync"
)

type Pool struct {
	Queue chan int
	Wg    *sync.WaitGroup
}

func New(size int) *Pool {
	if size <= 0 {
		size = 1
	}
	return &Pool{
		Queue: make(chan int, size),
		Wg:    &sync.WaitGroup{},
	}
}

func (p *Pool) Add(delta int) {
	for i := 0; i < delta; i++ {
		p.Queue <- 1
	}
	for i := 0; i > delta; i-- {
		<-p.Queue
	}
	p.Wg.Add(delta)
}

func (p *Pool) Done() {
	<-p.Queue
	p.Wg.Done()
}

func (p *Pool) Wait() {
	p.Wg.Wait()
}