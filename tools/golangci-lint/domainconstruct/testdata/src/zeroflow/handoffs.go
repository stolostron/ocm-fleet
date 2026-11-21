package zeroflow

import (
	"errors"
	"iter"
	"sync"
)

func CollectionAfterHandoff(valid ID) []ID {
	values := []ID{valid}
	requests := make(chan []ID)
	done := make(chan struct{})
	go func() {
		request := <-requests
		request[0] = ""
		close(done)
	}()
	requests <- values
	<-done
	return values // want "return may contain an empty zeroflow.ID"
}

func RepairCollectionAfterHandoff(valid ID) []ID {
	values := []ID{valid}
	requests := make(chan []ID)
	done := make(chan struct{})
	go func() {
		request := <-requests
		request[0] = ""
		close(done)
	}()
	requests <- values
	<-done
	values[0] = valid
	return values
}

func PrivateCollectionAfterHandoff(valid ID) []ID {
	values := []ID{valid}
	done := make(chan struct{})
	go close(done)
	<-done
	return values
}

func publishFillTarget(values []ID) { published = values }

func FillAfterPublishingCall(valid ID, writes chan<- int, ack <-chan struct{}) []ID {
	values := []ID{valid}
	publishFillTarget(values)
	for index := range values {
		values[index] = valid
		writes <- index
		<-ack
	}
	return values // want "return may contain an empty zeroflow.ID"
}

var publishedMap = make(map[int][]ID)

func FillAfterMapPublication(valid ID, writes chan<- int, ack <-chan struct{}) []ID {
	values := []ID{valid}
	publishedMap[0] = values
	for index := range values {
		values[index] = valid
		writes <- index
		<-ack
	}
	return values // want "return may contain an empty zeroflow.ID"
}

func FillAfterBorrowedMapPublication(shared map[int][]ID, valid ID, writes chan<- int, ack <-chan struct{}) []ID {
	values := []ID{valid}
	shared[0] = values
	for index := range values {
		values[index] = valid
		writes <- index
		<-ack
	}
	return values // want "return may contain an empty zeroflow.ID"
}

func FillAfterPrivateMapInsertion(valid ID, writes chan<- int, ack <-chan struct{}) []ID {
	values := []ID{valid}
	private := make(map[int][]ID)
	private[0] = values
	for index := range values {
		values[index] = valid
		writes <- index
		<-ack
	}
	return values
}

func WaitedForErrorWrite() (ID, error) {
	var lock sync.Mutex
	var worker sync.WaitGroup
	statuses := make([]error, 1)
	lock.Lock()
	worker.Add(1)
	go func(out []error) {
		lock.Lock()
		out[0] = nil
		lock.Unlock()
		worker.Done()
	}(statuses)
	statuses[0] = errors.New("initial")
	lock.Unlock()
	worker.Wait()
	return "", errors.Join(statuses...) // want "return produces an empty zeroflow.ID"
}

func WaitWithoutSharedErrors(worker *sync.WaitGroup) (ID, error) {
	statuses := []error{errors.New("private")}
	worker.Wait()
	return "", errors.Join(statuses...)
}

func ReadAfterWorkerClearsValue(use func(ID)) {
	var lock sync.Mutex
	var worker sync.WaitGroup
	raw := "ready"
	lock.Lock()
	worker.Add(1)
	go func() {
		lock.Lock()
		raw = ""
		lock.Unlock()
		worker.Done()
	}()
	ready := raw != ""
	lock.Unlock()
	worker.Wait()
	if ready {
		use(ID(raw)) // want "argument may be an empty zeroflow.ID"
		raw = "repaired"
		use(ID(raw))
	}
}

func ReadPrivateFieldAfterWait(raw string, worker *sync.WaitGroup, use func(ID)) {
	value := struct{ raw string }{raw: raw}
	if value.raw != "" {
		worker.Wait()
		use(ID(value.raw))
	}
}

func ReadAfterWaitGroupGoClearsValue(use func(ID)) {
	var lock sync.Mutex
	var worker sync.WaitGroup
	raw := "ready"
	lock.Lock()
	worker.Go(func() {
		lock.Lock()
		raw = ""
		lock.Unlock()
	})
	ready := raw != ""
	lock.Unlock()
	worker.Wait()
	if ready {
		use(ID(raw)) // want "argument may be an empty zeroflow.ID"
	}
}

func IteratorAfterFlagHandoff(values iter.Seq[ID], work chan<- *bool, ack <-chan struct{}, use func(ID)) {
	var id ID
	found := false
	work <- &found
	<-ack
	for value := range values {
		id, found = value, true
	}
	if found {
		use(id) // want "argument may be an empty zeroflow.ID"
	}
}

func IteratorAfterHandoffReset(values iter.Seq[ID], work chan<- *bool, ack <-chan struct{}, use func(ID)) {
	var id ID
	found := false
	work <- &found
	<-ack
	found = false
	for value := range values {
		id, found = value, true
	}
	if found {
		use(id)
	}
}
