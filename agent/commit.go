package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"
)

// commitTimeout bounds a turn's closing work (the footer, the pending-action
// refresh and the commit), which runs even after the turn was cancelled
// (adr/0231).
const commitTimeout = 30 * time.Second

// toolTimeout bounds one dispatched tool, which runs on through a cancel
// (adr/0239). Each backend request has its own, shorter limit.
const toolTimeout = 2 * time.Minute

// ErrTurnTimedOut is the cancel cause of a turn stopped by its time limit, so
// the record does not say the analyst stopped it (adr/0239).
var ErrTurnTimedOut = errors.New("turn timed out")

// defaultCommitBackoff is the wait before each retry of a turn commit that
// failed in transit.
var defaultCommitBackoff = []time.Duration{time.Second, 4 * time.Second}

// errTurnHeld marks a turn whose commit could not reach the backend: the
// session holds it and commits it before the next turn's own (adr/0231).
var errTurnHeld = errors.New("the turn is held and will be recorded with the next turn")

// commitTurn records req after any turns still held from earlier commits, in
// order, and returns the interpretation id req was recorded as. A commit that
// fails in transit is retried; one still failing is held with every turn after
// it, so the thread keeps its order. A commit the backend rejects is not
// retried and is not held: it will not succeed later.
func (s *Session) commitTurn(ctx context.Context, req InterpretationRequest) (string, error) {
	queue := append(slices.Clip(s.held), req)
	s.held = nil
	var errs []error
	for i, r := range queue {
		id, err := s.recordWithRetry(ctx, r)
		switch {
		case err == nil:
			if i == len(queue)-1 {
				return id, errors.Join(errs...)
			}
		case retryable(err):
			s.held = queue[i:]
			return "", errors.Join(append(errs, fmt.Errorf("agent: commit turn: %w; %w", err, errTurnHeld))...)
		case i == len(queue)-1:
			errs = append(errs, fmt.Errorf("agent: commit turn: %w", err))
		default:
			errs = append(errs, fmt.Errorf("agent: commit held turn: %w", err))
		}
	}
	return "", errors.Join(errs...)
}

// FlushHeld commits the turns held from earlier commits, best-effort. A reset
// marker must follow them on the thread, or rehydration would replay a turn
// the analyst reset away, and a session that is closing must not take them with
// it. Not safe to run alongside a turn.
func (s *Session) FlushHeld(ctx context.Context) error {
	queue := s.held
	s.held = nil
	for i, r := range queue {
		if _, err := s.recordWithRetry(ctx, r); err != nil && retryable(err) {
			s.held = queue[i:]
			return fmt.Errorf("agent: commit held turn: %w", err)
		}
	}
	return nil
}

// recordWithRetry is one turn commit, retried while it fails in transit. The
// backend recognises a repeat of a turn already recorded (by its turn id) and
// answers with the original interpretation, so a retry after a lost response
// never records the turn twice.
func (s *Session) recordWithRetry(ctx context.Context, req InterpretationRequest) (string, error) {
	for attempt := 0; ; attempt++ {
		interp, err := s.backend.RecordInterpretation(ctx, req)
		if err == nil {
			return interp.InterpretationID, nil
		}
		if !retryable(err) || attempt >= len(s.commitBackoff) {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", err
		case <-time.After(s.commitBackoff[attempt]):
		}
	}
}

// retryable reports whether a commit failure may succeed later: anything but a
// backend answer refusing the request.
func retryable(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return true
	}
	switch apiErr.Status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	}
	return apiErr.Status >= 500
}
