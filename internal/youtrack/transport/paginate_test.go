package transport

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// collection serves items like a YouTrack collection whose server caps $top
// at maxTop (0 for no cap) and records each request as "skip/top".
type collection struct {
	items  []int
	maxTop int
	calls  []string
}

func (c *collection) fetch(skip, top int) ([]int, error) {
	c.calls = append(c.calls, fmt.Sprintf("%d/%d", skip, top))
	if c.maxTop > 0 {
		top = min(top, c.maxTop)
	}
	var page []int
	for i := skip; i < len(c.items) && i < skip+top; i++ {
		page = append(page, c.items[i])
	}
	return page, nil
}

func seq(n int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = i
	}
	return s
}

func TestPaginate(t *testing.T) {
	tests := []struct {
		name                  string
		size, maxTop          int
		skip, pageSize, limit int
		wantLen, wantFirst    int
		wantCalls             []string
	}{
		{"a limit below one page", 50, 0, 0, 100, 7, 7, 0, []string{"0/7"}},
		{"a non-zero starting skip", 10, 0, 4, 3, 0, 6, 4, []string{"4/3", "7/3", "10/3"}},
		{"a limit spanning pages", 250, 0, 0, 100, 150, 150, 0, []string{"0/100", "100/50"}},
		{"no limit over several pages", 250, 0, 0, 100, 0, 250, 0, []string{"0/100", "100/100", "200/100"}},
		{"a server cap below the page size", 100, 30, 0, 100, 0, 100, 0, []string{"0/100", "30/100", "60/100", "90/100"}},
		{"a collection that ends on a page boundary", 200, 0, 0, 100, 0, 200, 0, []string{"0/100", "100/100", "200/100"}},
	}
	for _, tt := range tests {
		t.Run("given "+tt.name+", it fetches "+fmt.Sprint(tt.wantCalls), func(t *testing.T) {
			c := &collection{items: seq(tt.size), maxTop: tt.maxTop}
			got, err := Paginate(tt.skip, tt.pageSize, tt.limit, c.fetch)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tt.wantLen || (tt.wantLen > 0 && got[0] != tt.wantFirst) {
				t.Errorf("got %d items starting %v", len(got), got)
			}
			if !reflect.DeepEqual(c.calls, tt.wantCalls) {
				t.Errorf("calls = %v, want %v", c.calls, tt.wantCalls)
			}
		})
	}

	t.Run("given an empty first page, it returns an empty, non-nil slice", func(t *testing.T) {
		c := &collection{}
		got, err := Paginate(0, 10, 0, c.fetch)
		if err != nil || got == nil || len(got) != 0 {
			t.Errorf("got %#v, %v", got, err)
		}
		if len(c.calls) != 1 {
			t.Errorf("calls = %v; an empty page ends paging", c.calls)
		}
	})

	t.Run("given a fetch error, it stops and returns it", func(t *testing.T) {
		boom := errors.New("boom")
		calls := 0
		_, err := Paginate(0, 10, 0, func(_, _ int) ([]int, error) {
			calls++
			if calls == 2 {
				return nil, boom
			}
			return seq(10), nil
		})
		if !errors.Is(err, boom) || calls != 2 {
			t.Errorf("err = %v after %d calls", err, calls)
		}
	})
}
