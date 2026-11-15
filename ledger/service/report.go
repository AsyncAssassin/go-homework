package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/domain"
)

const reportHeartbeatInterval = 400 * time.Millisecond

// GetReportSummary includes both boundary days in the local time zone.
// On a cache miss each category is summed by a separate goroutine. All workers
// and the heartbeat finish before this method returns, including on cancellation.
func (s *ledgerService) GetReportSummary(ctx context.Context, from, to time.Time) (domain.ReportSummary, error) {
	from, to = domain.StartOfDay(from), domain.StartOfDay(to)
	if err := domain.CheckDay(from); err != nil {
		return domain.ReportSummary{}, err
	}
	if err := domain.CheckDay(to); err != nil {
		return domain.ReportSummary{}, err
	}
	if to.Before(from) {
		return domain.ReportSummary{}, fmt.Errorf("%w: from %s is after to %s", domain.ErrInvalidRange, formatDate(from), formatDate(to))
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return domain.ReportSummary{}, err
	}

	var totals []domain.CategoryTotal
	var found bool
	if s.reports != nil {
		totals, found = s.reports.GetReport(ctx, from, to)
	}
	if err := ctx.Err(); err != nil {
		return domain.ReportSummary{}, err
	}
	if !found {
		var err error
		totals, err = s.calculateReport(ctx, from, to)
		if err != nil {
			return domain.ReportSummary{}, err
		}
		if s.reports != nil {
			s.reports.SetReport(ctx, from, to, totals)
		}
		if err := ctx.Err(); err != nil {
			return domain.ReportSummary{}, err
		}
	}
	report := domain.ReportSummary{From: from, To: to, Categories: totals}
	for _, total := range totals {
		report.Total += total.Amount
	}
	report.Total = roundToKopecks(report.Total)
	return report, nil
}

func (s *ledgerService) calculateReport(ctx context.Context, from, to time.Time) ([]domain.CategoryTotal, error) {
	workCtx, cancel := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(reportHeartbeatInterval)
		defer ticker.Stop()
		ticked := false
		for {
			select {
			case <-ticker.C:
				log.Print("report calculation in progress")
				ticked = true
			case <-workCtx.Done():
				if ticked || ctx.Err() != nil {
					log.Print("report heartbeat stopped")
				}
				return
			}
		}
	}()
	defer func() {
		cancel()
		<-heartbeatDone
		if err := ctx.Err(); err != nil {
			log.Printf("report calculation canceled: %v", err)
		}
	}()

	// Calendar arithmetic preserves the last day across daylight-saving changes.
	y, m, d := to.Date()
	until := domain.Day(y, m, d+1)
	categories, err := s.expenses.ListCategories(workCtx, from, until)
	if err != nil {
		return nil, err
	}
	if err := workCtx.Err(); err != nil {
		return nil, err
	}
	totals := make([]domain.CategoryTotal, len(categories))
	errs := make([]error, len(categories))
	var workers sync.WaitGroup
	workers.Add(len(categories))
	for i, category := range categories {
		go func() {
			defer workers.Done()
			if err := workCtx.Err(); err != nil {
				errs[i] = err
				return
			}
			amount, err := s.expenses.SumByCategory(workCtx, category, from, until)
			if err != nil {
				errs[i] = err
				cancel()
				return
			}
			// Each worker owns its index; callers read only after Wait.
			totals[i] = domain.CategoryTotal{Category: category, Amount: amount}
		}()
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Preserve the original storage failure instead of a sibling's cancellation.
	for _, err := range errs {
		if err != nil && !errors.Is(err, context.Canceled) {
			return nil, err
		}
	}
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return totals, nil
}
