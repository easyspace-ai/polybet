package httpserver

import (
	"errors"
	"net"
	"testing"

	"github.com/gorilla/websocket"
)

func TestClassifyWSReadErr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		reason wsReadEndReason
	}{
		{
			name:   "nil",
			err:    nil,
			reason: "",
		},
		{
			name:   "client close",
			err:    &websocket.CloseError{Code: websocket.CloseNormalClosure},
			reason: wsReadEndClientClosed,
		},
		{
			name:   "idle read deadline",
			err:    &net.OpError{Op: "read", Err: &timeoutErr{}},
			reason: wsReadEndIdleTimeout,
		},
		{
			name:   "unexpected error",
			err:    errors.New("broken pipe"),
			reason: wsReadEndError,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyWSReadErr(tc.err); got != tc.reason {
				t.Fatalf("classifyWSReadErr() = %q, want %q", got, tc.reason)
			}
		})
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false }

func TestClassifyWSReadErr_IOTimeoutString(t *testing.T) {
	t.Parallel()
	err := &net.OpError{
		Op:  "read",
		Net: "tcp",
		Err: timeoutErr{},
	}
	if got := classifyWSReadErr(err); got != wsReadEndIdleTimeout {
		t.Fatalf("classifyWSReadErr() = %q, want %q", got, wsReadEndIdleTimeout)
	}
}
