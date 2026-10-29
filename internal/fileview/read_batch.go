package fileview

import "sync"

// ReadFiles visits sources in input order. Only an entire batch declared as
// immutable Git blobs on a pinned View is prefetched with bounded concurrency.
// Disk and mixed batches retain their original read/consume ordering. Every
// started read is joined before returning, including on failure/cancellation.
func ReadFiles(files Reader, paths []string, consume func(int, []byte, error) error) error {
	view, pinned := files.(*View)
	if pinned {
		pinned = pinnedCommit(view.Commit)
		for _, path := range paths {
			source, err := view.Source(path)
			if err != nil || source != "git_tree" {
				pinned = false
				break
			}
		}
	}
	if !pinned || len(paths) < 2 {
		for i, path := range paths {
			data, err := files.ReadFile(path)
			if err := consume(i, data, err); err != nil {
				return err
			}
		}
		return nil
	}
	type result struct {
		data []byte
		err  error
	}
	results := make([]result, len(paths))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(4, len(paths)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				results[i].data, results[i].err = view.ReadFile(paths[i])
			}
		}()
	}
	for i := range paths {
		if view.context().Err() != nil {
			break
		}
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	if err := view.context().Err(); err != nil {
		return err
	}
	for i, result := range results {
		if err := consume(i, result.data, result.err); err != nil {
			return err
		}
	}
	return nil
}

func pinnedCommit(commit string) bool {
	if len(commit) != 40 && len(commit) != 64 {
		return false
	}
	for _, c := range commit {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
