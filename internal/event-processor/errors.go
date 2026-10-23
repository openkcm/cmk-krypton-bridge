package eventprocessor

import (
	"context"
	"errors"
	"log/slog"

	"github.com/openkcm/orbital"
)

var (
	ErrInvalidJobType     = errors.New("invalid job type")
	ErrUnsupportedJobType = errors.New("unsupported job type")
)

// setWorkingState mirrors CMK's setWorkingState.
func setWorkingState(ctx context.Context, resp *orbital.HandlerResponse, err error, state string) {
	ws, getErr := resp.WorkingState()
	if getErr != nil || ws == nil {
		slog.ErrorContext(ctx, "orbital: failed to get working state", slog.String("error", getErr.Error()))
		return
	}
	ws.Set("state", state)
	ws.Set("error", err.Error())
}

// setErrorStateAndFail mirrors CMK's setErrorStateAndFail.
func setErrorStateAndFail(ctx context.Context, resp *orbital.HandlerResponse, err error, state string) {
	slog.ErrorContext(ctx, "orbital: task failed, ending processing", slog.String("error", err.Error()), slog.String("state", state))
	setWorkingState(ctx, resp, err, state)
	resp.Fail(err.Error())
}
