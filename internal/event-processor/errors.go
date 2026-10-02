package eventprocessor

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"

	"github.com/openkcm/orbital"
)

const reconcileAfterError = 15 * time.Second

var (
	ErrInvalidJobType     = errors.New("invalid job type")
	ErrUnsupportedJobType = errors.New("unsupported job type")
)

// setWorkingState mirrors CMK's setWorkingState.
func setWorkingState(ctx context.Context, lg *zap.Logger, resp *orbital.HandlerResponse, err error, state string) {
	ws, getErr := resp.WorkingState()
	if getErr != nil || ws == nil {
		lg.Error("orbital: failed to get working state", zap.Error(getErr))
		return
	}
	ws.Set("state", state)
	ws.Set("error", err.Error())
}

// setErrorStateAndFail mirrors CMK's setErrorStateAndFail.
func setErrorStateAndFail(ctx context.Context, lg *zap.Logger, resp *orbital.HandlerResponse, err error, state string) {
	lg.Error("orbital: task failed, ending processing", zap.Error(err), zap.String("state", state))
	setWorkingState(ctx, lg, resp, err, state)
	resp.Fail(err.Error())
}

// setErrorStateAndContinue mirrors CMK's setErrorStateAndContinue.
func setErrorStateAndContinue(ctx context.Context, lg *zap.Logger, resp *orbital.HandlerResponse, err error, state string) {
	lg.Error("orbital: task failed, will retry", zap.Error(err), zap.String("state", state))
	setWorkingState(ctx, lg, resp, err, state)
	resp.ContinueAndWaitFor(reconcileAfterError)
}
