package gh

import "sync"

// defaultConcurrency bounds parallel API calls. GitHub tolerates far more, but
// there is no point flooding it — the slow part is the network, not us.
const defaultConcurrency = 10

// Result pairs a job's output with its error, keeping the input order.
type Result[T any] struct {
	Value T
	Err   error
}

// fanOut runs fn over items concurrently and returns results in input order.
// Order matters because the UI groups by repo and a shuffled list looks like a
// bug.
func fanOut[In, Out any](items []In, workers int, fn func(In) (Out, error)) []Result[Out] {
	if workers <= 0 {
		workers = defaultConcurrency
	}
	if workers > len(items) {
		workers = len(items)
	}

	results := make([]Result[Out], len(items))
	if len(items) == 0 {
		return results
	}

	type job struct {
		idx  int
		item In
	}
	jobs := make(chan job)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				v, err := fn(j.item)
				results[j.idx] = Result[Out]{Value: v, Err: err}
			}
		}()
	}

	for i, it := range items {
		jobs <- job{idx: i, item: it}
	}
	close(jobs)
	wg.Wait()

	return results
}
