package strategy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"optionsbot/internal/orders"
)

// Manual rebalance from the monitor (POST /api/rebalance). The automatic
// rebalance runs only when the confirmed IM limit changes (and at startup), so
// a deposit or withdrawal — which moves the margin balance, not the limit —
// left the book sized for the old balance until a restart. A manual request
// runs the same pass with the same gates (RebalanceBlockReason); it only skips
// "the limit has not changed" and the retry wait, which are exactly why it is
// asked for. A dry run reports what the pass would do and sends nothing.

const (
	// manualRebalanceCooldown spaces real manual passes, so repeated clicks
	// cannot churn the book.
	manualRebalanceCooldown = 5 * time.Minute
	// manualDryRunCooldown spaces dry runs: each strangle costs several
	// margin simulations, and Deribit allows one a second.
	manualDryRunCooldown = 30 * time.Second
	// rebalancePickup is how long a request may wait for the loop to take it.
	rebalancePickup = 25 * time.Second
)

// ErrRebalanceBusy: the decision loop did not take the request in time.
var ErrRebalanceBusy = errors.New("the decision loop did not take the request in time; nothing was sent")

// RebalanceRefused: the gates do not allow a rebalance now. Nothing was sent.
type RebalanceRefused struct{ Reason string }

func (e RebalanceRefused) Error() string { return "rebalance refused: " + e.Reason }

// RebalanceCooldown: a manual pass ran too recently.
type RebalanceCooldown struct{ RetryAfter time.Duration }

func (e RebalanceCooldown) Error() string {
	return fmt.Sprintf("a manual rebalance ran recently; try again in %s", e.RetryAfter.Round(time.Second))
}

type rebalanceReq struct {
	dryRun   bool
	by       string
	deadline time.Time // not started after this: the caller has given up
	reply    chan rebalanceReply
}

type rebalanceReply struct {
	report RebalanceReport
	err    error
}

// ManualRebalanceWait returns how long a caller must still wait before the
// next manual pass of this kind, or 0. Pure.
func ManualRebalanceWait(last, now time.Time, dryRun bool) time.Duration {
	cool := manualRebalanceCooldown
	if dryRun {
		cool = manualDryRunCooldown
	}
	if last.IsZero() {
		return 0
	}
	if wait := last.Add(cool).Sub(now); wait > 0 {
		return wait
	}
	return 0
}

// RequestRebalance asks the decision loop for a rebalance pass (or a dry run)
// and waits for its report. Safe from any goroutine. A request the loop has
// not taken within rebalancePickup is dropped unexecuted, so a caller that
// gave up never rebalances later.
func (s *Strategy) RequestRebalance(ctx context.Context, dryRun bool, by string) (RebalanceReport, error) {
	s.pub.mu.RLock()
	halted := s.pub.halted
	s.pub.mu.RUnlock()
	if halted {
		return RebalanceReport{}, RebalanceRefused{Reason: "the kill switch has fired: the bot is idle"}
	}
	if !dryRun { // dry runs are spaced on the loop (runManualRebalance)
		s.manualRebMu.Lock()
		wait := ManualRebalanceWait(s.lastManualReb, time.Now(), false)
		s.manualRebMu.Unlock()
		if wait > 0 {
			return RebalanceReport{}, RebalanceCooldown{RetryAfter: wait}
		}
	}
	req := rebalanceReq{dryRun: dryRun, by: by, deadline: time.Now().Add(rebalancePickup), reply: make(chan rebalanceReply, 1)}
	select {
	case s.rebalanceCh <- req:
	default:
		return RebalanceReport{}, errors.New("another rebalance request is waiting; try again")
	}
	pickup := time.NewTimer(rebalancePickup + 5*time.Second)
	defer pickup.Stop()
	for {
		select {
		case r := <-req.reply:
			return r.report, r.err
		case <-ctx.Done():
			return RebalanceReport{}, ctx.Err()
		case <-pickup.C:
			// Taken but still running (simulations are a second each): keep
			// waiting on ctx; not taken: it is dropped at its deadline.
			pickup.Reset(time.Hour)
		}
	}
}

// runManualRebalance executes a request on the Run goroutine.
func (s *Strategy) runManualRebalance(ctx context.Context, req rebalanceReq) {
	if time.Now().After(req.deadline) {
		slog.Warn("manual rebalance dropped: taken after its deadline", "by", req.by, "dry_run", req.dryRun)
		req.reply <- rebalanceReply{err: ErrRebalanceBusy}
		return
	}
	if req.dryRun {
		if wait := ManualRebalanceWait(s.lastDryRun, time.Now(), true); wait > 0 {
			req.reply <- rebalanceReply{err: RebalanceCooldown{RetryAfter: wait}}
			return
		}
		s.lastDryRun = time.Now()
	}

	m := s.marginNow(ctx, s.gamma.Evaluate())
	if why := s.rebalanceBlockReason(m); why != "" {
		slog.Warn("manual rebalance refused", "by", req.by, "dry_run", req.dryRun, "reason", why)
		req.reply <- rebalanceReply{err: RebalanceRefused{Reason: why}}
		return
	}

	if req.dryRun {
		rep := s.rebalancePositions(ctx, m, true)
		slog.Info("manual rebalance dry run", "by", req.by, "actions", len(rep.Actions))
		req.reply <- rebalanceReply{report: rep}
		return
	}

	defer s.publish()
	s.manualRebMu.Lock()
	s.lastManualReb = time.Now()
	s.manualRebMu.Unlock()
	slog.Warn("manual rebalance requested", "by", req.by,
		"limit_im_pct", m.status.LimitIMPct, "im_pct", fmt.Sprintf("%.2f", m.usage.IMPct()), "unit", m.usage.Unit)
	s.logRisk(orders.RiskRebalanceManual,
		fmt.Sprintf("manual rebalance from the monitor (%s): IM %.1f%% vs limit %.0f%%, margin balance %.6f %s",
			req.by, m.usage.IMPct(), m.status.LimitIMPct, m.usage.MarginBalance, m.usage.Unit),
		m.status, m.usage, "")
	rep := s.runRebalance(ctx, m)
	slog.Warn("manual rebalance done", "by", req.by, "done", rep.Done, "actions", len(rep.Actions))
	req.reply <- rebalanceReply{report: rep}
}
