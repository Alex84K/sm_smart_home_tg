package app

import "time"

// SetAfterFunc replaces the clip auto-stop scheduler in tests.
func (a *App) SetAfterFunc(f func(d time.Duration, fn func()) *time.Timer) {
	a.afterFunc = f
}
