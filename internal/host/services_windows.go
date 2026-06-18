//go:build windows

package host

// Services enumerates managed background services. Not yet implemented on
// Windows (would use the Windows Service Control Manager); returns none.
func Services() []Service { return nil }
