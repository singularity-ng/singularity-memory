package worker

import (
	"context"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jackc/pgx/v5"

	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/modelrouter"
	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/store"
)

// Store is the subset of the store interface the worker needs.
type Store interface {
	ListBanks(ctx context.Context) ([]store.BankListItem, error)
	ClaimBrainJob(ctx context.Context, bankID string, kinds []string) (*store.BrainJob, error)
	CompleteBrainJob(ctx context.Context, bankID string, jobID string, status string, result map[string]any, jobErr *string) (*store.BrainJob, error)
	ReflectAgentMemory(ctx context.Context, bankID string, limit int) (*store.Reflection, error)
	RunDeterministicConsolidation(ctx context.Context, bankID string, limit int) (*store.ConsolidationResult, error)
	InsertMemoryUnit(ctx context.Context, bankID string, unit *store.MemoryUnit) (string, error)
	UpsertCoreMemoryBlock(ctx context.Context, block store.CoreMemoryBlock) (*store.CoreMemoryBlock, error)
	AppendCoreMemoryBlock(ctx context.Context, bankID string, blockName string, text string) (*store.CoreMemoryBlock, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Worker struct {
	store        Store
	router       *modelrouter.Router
	logger       *log.Logger
	concurrency  int
	pollInterval time.Duration
	sharedBankID string // propagate high-confidence observations here
}

// New creates a Worker.
func New(store Store, router *modelrouter.Router, logger *log.Logger, concurrency int, pollInterval time.Duration, sharedBankID string) *Worker {
	if concurrency <= 0 {
		concurrency = 2
	}
	if pollInterval <= 0 {
		pollInterval = 30 * time.Second
	}
	return &Worker{
		store:        store,
		router:       router,
		logger:       logger,
		concurrency:  concurrency,
		pollInterval: pollInterval,
		sharedBankID: sharedBankID,
	}
}

// Start begins polling. It blocks until ctx is cancelled.
func (w *Worker) Start(ctx context.Context) {
	sem := make(chan struct{}, w.concurrency)
	var wg sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		default:
		}

		banks, err := w.store.ListBanks(ctx)
		if err != nil {
			w.logger.Warn("worker: list banks failed", "error", err)
			select {
			case <-ctx.Done():
				wg.Wait()
				return
			case <-time.After(w.pollInterval):
			}
			continue
		}

		kinds := []string{"sleep", "hindsight", "consolidate"}
		found := false
		for _, bank := range banks {
			bankID := bank.BankID
			job, err := w.store.ClaimBrainJob(ctx, bankID, kinds)
			if err != nil || job == nil {
				continue
			}
			found = true

			sem <- struct{}{}
			wg.Add(1)
			go func(bID string, j *store.BrainJob) {
				defer func() { <-sem; wg.Done() }()
				w.dispatch(ctx, bID, j)
			}(bankID, job)
		}

		if !found {
			select {
			case <-ctx.Done():
				wg.Wait()
				return
			case <-time.After(w.pollInterval):
			}
		}
	}
}

func (w *Worker) dispatch(ctx context.Context, bankID string, job *store.BrainJob) {
	var handlerErr error

	switch job.Kind {
	case "consolidate":
		_, handlerErr = w.store.RunDeterministicConsolidation(ctx, bankID, 50)
	case "sleep":
		route, ok := w.router.Route(ctx, modelrouter.TaskConsolidateBank)
		if !ok {
			w.logger.Info("worker: skipping sleep job — no LLM configured", "bank_id", bankID, "job_id", job.ID)
			return
		}
		handlerErr = handleSleep(ctx, bankID, job, w.store, route, w.logger, w.sharedBankID)
	case "hindsight":
		route, ok := w.router.Route(ctx, modelrouter.TaskHindsight)
		if !ok {
			w.logger.Info("worker: skipping hindsight job — no LLM configured", "bank_id", bankID, "job_id", job.ID)
			return
		}
		handlerErr = handleHindsight(ctx, bankID, job, w.store, route, w.logger)
	default:
		w.logger.Warn("worker: unknown job kind", "kind", job.Kind, "bank_id", bankID, "job_id", job.ID)
		return
	}

	status := "done"
	var jobErrPtr *string
	if handlerErr != nil {
		status = "failed"
		errStr := handlerErr.Error()
		jobErrPtr = &errStr
		w.logger.Error("worker: job failed", "kind", job.Kind, "bank_id", bankID, "job_id", job.ID, "error", handlerErr)
	} else {
		w.logger.Info("worker: job completed", "kind", job.Kind, "bank_id", bankID, "job_id", job.ID)
	}

	if _, err := w.store.CompleteBrainJob(ctx, bankID, job.ID, status, nil, jobErrPtr); err != nil {
		w.logger.Error("worker: complete job failed", "job_id", job.ID, "error", err)
	}
}
