package root

import "time"

// SetNoticeGrace widens the update-notice grace for a test.
func SetNoticeGrace(d time.Duration) (restore func()) {
	old := noticeGrace
	noticeGrace = d
	return func() { noticeGrace = old }
}
