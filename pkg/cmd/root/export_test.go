package root

import "time"

// SetNoticeGrace widens the update-notice grace for a test.
func SetNoticeGrace(d time.Duration) (restore func()) {
	old := noticeGrace
	noticeGrace = d
	return func() { noticeGrace = old }
}

// SetSkillSync turns the background skill refresh on or off for a test.
func SetSkillSync(on bool) (restore func()) {
	old := skillSyncOff
	skillSyncOff = !on
	return func() { skillSyncOff = old }
}
