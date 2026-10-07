package transport

// PageFetcher fetches the page of up to top items starting at skip.
type PageFetcher[T any] func(skip, top int) ([]T, error)

// Paginate collects items from a YouTrack $skip/$top collection, starting at
// skip with pages of pageSize. A positive limit stops after that many items
// and never asks for more than are still needed; zero fetches everything.
//
// It stops at an empty page, or at a page shorter than both what it asked
// for and the first page: a server may cap $top below the request, so a
// short first page alone does not prove the collection ended.
func Paginate[T any](skip, pageSize, limit int, fetch PageFetcher[T]) ([]T, error) {
	all := []T{}
	capSize := pageSize
	for first := true; ; first = false {
		top := pageSize
		if limit > 0 {
			top = min(top, limit-len(all))
		}
		page, err := fetch(skip, top)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if limit > 0 && len(all) >= limit {
			return all[:limit], nil
		}
		if len(page) == 0 || (!first && len(page) < min(top, capSize)) {
			return all, nil
		}
		if first {
			capSize = min(top, len(page))
		}
		skip += len(page)
	}
}
