package config

const (
	// BufSize is the size of buffers passed in to sockets
	BufSize = 8192
	// ConnSockBufSize is the size of the socket used for
	// to attach to the container
	ConnSockBufSize = 32768
	// WinResizeEvent is the event code the caller program will
	// send along the ctrl fd to signal conmon to resize
	// the pty window
	WinResizeEvent = 1
	// ReopenLogsEvent is the event code the caller program will
	// send along the ctrl fd to signal conmon to reopen the log files
	ReopenLogsEvent = 2
	// TimedOutMessage is the message sent back to the caller by conmon
	// when a container times out
	TimedOutMessage = "command timed out"
)
