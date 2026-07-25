package apicall

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/JungHoonGhae/gongctl/internal/fetch"
)

// propagationPoll is how often CallWaiting retries. Observed propagation runs 7–10
// minutes, so a shorter interval only adds requests the gateway will refuse; a
// longer one wastes minutes after it becomes ready. A variable so tests can run the
// retry logic without sleeping through it.
var propagationPoll = 60 * time.Second

// MaxPropagationWait bounds any caller's wait. The portal's own guidance says
// approval can take up to an hour, and waiting longer than that is no longer
// waiting for propagation — it is waiting for something that is not going to happen.
const MaxPropagationWait = time.Hour

// CallWaiting is Call, except that a gateway 403 — the approval has not reached
// apis.data.go.kr yet — is treated as "not yet" rather than a failure, and the
// request is repeated until it succeeds or wait runs out.
//
// This exists because the wait is unavoidable and the 403 is indistinguishable, to
// anyone who has not read the portal's behaviour, from a bad key: the application
// shows 승인 the moment it is made, and the API still refuses for minutes. Every
// other error returns immediately — a rejected key or a malformed request will not
// improve by being asked again, and retrying it would only obscure the real reason.
//
// notify, when non-nil, is called before each sleep with the elapsed time, so a
// caller can show progress rather than appearing to hang for ten minutes.
func CallWaiting(ctx context.Context, f *fetch.Client, endpoint string, params map[string]string,
	key string, wait time.Duration, notify func(elapsed, remaining time.Duration)) (*CallResult, error) {
	if wait > MaxPropagationWait {
		wait = MaxPropagationWait
	}
	start := time.Now()
	deadline := start.Add(wait)
	for attempt := 1; ; attempt++ {
		res, err := Call(ctx, f, endpoint, params, key)
		if !errors.Is(err, ErrPropagating) {
			return res, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return res, fmt.Errorf("%w — %s 동안 %d회 시도했지만 아직 반영되지 않았습니다. "+
				"승인 직후라면 조금 더 기다렸다 다시 호출하세요(포털 안내상 최대 1시간)",
				ErrPropagating, time.Since(start).Round(time.Second), attempt)
		}
		sleep := propagationPoll
		if remaining < sleep {
			sleep = remaining
		}
		if notify != nil {
			notify(time.Since(start).Round(time.Second), remaining.Round(time.Second))
		}
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		case <-time.After(sleep):
		}
	}
}
