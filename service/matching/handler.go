// Copyright (c) 2017 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package matching

import (
	"context"
	"sync"
	"time"

	"github.com/uber-go/tally"
	"github.com/uber/cadence/.gen/go/health"
	m "github.com/uber/cadence/.gen/go/matching"
	"github.com/uber/cadence/.gen/go/matching/matchingserviceserver"
	gen "github.com/uber/cadence/.gen/go/shared"
	"github.com/uber/cadence/common"
	"github.com/uber/cadence/common/cache"
	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/common/metrics"
	"github.com/uber/cadence/common/persistence"
	"github.com/uber/cadence/common/quotas"
	"github.com/uber/cadence/common/service"
)

var _ matchingserviceserver.Interface = (*Handler)(nil)

// Handler - Thrift handler inteface for history service
type Handler struct {
	taskPersistence persistence.TaskManager
	metadataMgr     persistence.MetadataManager
	engine          Engine
	config          *Config
	metricsClient   metrics.Client
	startWG         sync.WaitGroup
	domainCache     cache.DomainCache
	rateLimiter     quotas.Limiter
	service.Service
}

var (
	errMatchingHostThrottle = &gen.ServiceBusyError{Message: "Matching host rps exceeded"}
)

// NewHandler creates a thrift handler for the history service
func NewHandler(sVice service.Service, config *Config, taskPersistence persistence.TaskManager, metadataMgr persistence.MetadataManager) *Handler {
	handler := &Handler{
		Service:         sVice,
		taskPersistence: taskPersistence,
		metadataMgr:     metadataMgr,
		config:          config,
		rateLimiter: quotas.NewDynamicRateLimiter(func() float64 {
			return float64(config.RPS())
		}),
	}
	// prevent us from trying to serve requests before matching engine is started and ready
	handler.startWG.Add(1)
	return handler
}

// RegisterHandler register this handler, must be called before Start()
func (h *Handler) RegisterHandler() {
	h.Service.GetDispatcher().Register(matchingserviceserver.New(h))
}

// Start starts the handler
func (h *Handler) Start() error {
	h.Service.Start()

	h.domainCache = cache.NewDomainCache(h.metadataMgr, h.GetClusterMetadata(), h.GetMetricsClient(), h.GetLogger())
	h.domainCache.Start()
	h.metricsClient = h.Service.GetMetricsClient()
	h.engine = NewEngine(
		h.taskPersistence, h.GetClientBean().GetHistoryClient(), h.config, h.Service.GetLogger(), h.Service.GetMetricsClient(), h.domainCache,
	)
	h.startWG.Done()
	return nil
}

// Stop stops the handler
func (h *Handler) Stop() {
	h.engine.Stop()
	h.domainCache.Stop()
	h.taskPersistence.Close()
	h.metadataMgr.Close()
	h.Service.Stop()
}

// Health is for health check
func (h *Handler) Health(ctx context.Context) (*health.HealthStatus, error) {
	h.startWG.Wait()
	h.GetLogger().Debug("Matching service health check endpoint reached.")
	hs := &health.HealthStatus{Ok: true, Msg: common.StringPtr("matching good")}
	return hs, nil
}

// startRequestProfile initiates recording of request metrics
func (h *Handler) startRequestProfile(api string, scope int) tally.Stopwatch {
	h.startWG.Wait()
	sw := h.metricsClient.StartTimer(scope, metrics.CadenceLatency)
	h.metricsClient.IncCounter(scope, metrics.CadenceRequests)
	return sw
}

// AddActivityTask - adds an activity task.
func (h *Handler) AddActivityTask(ctx context.Context, addRequest *m.AddActivityTaskRequest) (retError error) {
	defer log.CapturePanic(h.GetLogger(), &retError)
	startT := time.Now()
	scope := metrics.MatchingAddActivityTaskScope
	sw := h.startRequestProfile("AddActivityTask", scope)
	defer sw.Stop()

	if ok := h.rateLimiter.Allow(); !ok {
		return h.handleErr(errMatchingHostThrottle, scope)
	}

	syncMatch, err := h.engine.AddActivityTask(ctx, addRequest)
	if syncMatch {
		h.metricsClient.RecordTimer(scope, metrics.SyncMatchLatency, time.Since(startT))
	}

	return h.handleErr(err, scope)
}

// AddDecisionTask - adds a decision task.
func (h *Handler) AddDecisionTask(ctx context.Context, addRequest *m.AddDecisionTaskRequest) (retError error) {
	defer log.CapturePanic(h.GetLogger(), &retError)
	startT := time.Now()
	scope := metrics.MatchingAddDecisionTaskScope
	sw := h.startRequestProfile("AddDecisionTask", scope)
	defer sw.Stop()

	if ok := h.rateLimiter.Allow(); !ok {
		return h.handleErr(errMatchingHostThrottle, scope)
	}

	syncMatch, err := h.engine.AddDecisionTask(ctx, addRequest)
	if syncMatch {
		h.metricsClient.RecordTimer(scope, metrics.SyncMatchLatency, time.Since(startT))
	}
	return h.handleErr(err, scope)
}

// PollForActivityTask - long poll for an activity task.
func (h *Handler) PollForActivityTask(ctx context.Context,
	pollRequest *m.PollForActivityTaskRequest) (resp *gen.PollForActivityTaskResponse, retError error) {
	defer log.CapturePanic(h.GetLogger(), &retError)

	scope := metrics.MatchingPollForActivityTaskScope
	sw := h.startRequestProfile("PollForActivityTask", scope)
	defer sw.Stop()

	if ok := h.rateLimiter.Allow(); !ok {
		return nil, h.handleErr(errMatchingHostThrottle, scope)
	}

	if err := common.ValidateLongPollContextTimeout(ctx, "PollForActivityTask", h.Service.GetLogger()); err != nil {
		return nil, h.handleErr(err, scope)
	}

	response, err := h.engine.PollForActivityTask(ctx, pollRequest)
	return response, h.handleErr(err, scope)
}

// PollForDecisionTask - long poll for a decision task.
func (h *Handler) PollForDecisionTask(ctx context.Context,
	pollRequest *m.PollForDecisionTaskRequest) (resp *m.PollForDecisionTaskResponse, retError error) {
	defer log.CapturePanic(h.GetLogger(), &retError)

	scope := metrics.MatchingPollForDecisionTaskScope
	sw := h.startRequestProfile("PollForDecisionTask", scope)
	defer sw.Stop()

	if ok := h.rateLimiter.Allow(); !ok {
		return nil, h.handleErr(errMatchingHostThrottle, scope)
	}

	if err := common.ValidateLongPollContextTimeout(ctx, "PollForDecisionTask", h.Service.GetLogger()); err != nil {
		return nil, h.handleErr(err, scope)
	}

	response, err := h.engine.PollForDecisionTask(ctx, pollRequest)
	return response, h.handleErr(err, scope)
}

// QueryWorkflow queries a given workflow synchronously and return the query result.
func (h *Handler) QueryWorkflow(ctx context.Context,
	queryRequest *m.QueryWorkflowRequest) (resp *gen.QueryWorkflowResponse, retError error) {
	defer log.CapturePanic(h.GetLogger(), &retError)
	scope := metrics.MatchingQueryWorkflowScope
	sw := h.startRequestProfile("QueryWorkflow", scope)
	defer sw.Stop()

	if ok := h.rateLimiter.Allow(); !ok {
		return nil, h.handleErr(errMatchingHostThrottle, scope)
	}

	response, err := h.engine.QueryWorkflow(ctx, queryRequest)
	return response, h.handleErr(err, scope)
}

// RespondQueryTaskCompleted responds a query task completed
func (h *Handler) RespondQueryTaskCompleted(ctx context.Context, request *m.RespondQueryTaskCompletedRequest) (retError error) {
	defer log.CapturePanic(h.GetLogger(), &retError)
	scope := metrics.MatchingRespondQueryTaskCompletedScope
	sw := h.startRequestProfile("RespondQueryTaskCompleted", scope)
	defer sw.Stop()

	// Count the request in the RPS, but we still accept it even if RPS is exceeded
	h.rateLimiter.Allow()

	err := h.engine.RespondQueryTaskCompleted(ctx, request)
	return h.handleErr(err, scope)
}

// CancelOutstandingPoll is used to cancel outstanding pollers
func (h *Handler) CancelOutstandingPoll(ctx context.Context,
	request *m.CancelOutstandingPollRequest) (retError error) {
	defer log.CapturePanic(h.GetLogger(), &retError)
	scope := metrics.MatchingCancelOutstandingPollScope
	sw := h.startRequestProfile("CancelOutstandingPoll", scope)
	defer sw.Stop()

	// Count the request in the RPS, but we still accept it even if RPS is exceeded
	h.rateLimiter.Allow()

	err := h.engine.CancelOutstandingPoll(ctx, request)
	return h.handleErr(err, scope)
}

// DescribeTaskList returns information about the target tasklist, right now this API returns the
// pollers which polled this tasklist in last few minutes. If includeTaskListStatus field is true,
// it will also return status of tasklist's ackManager (readLevel, ackLevel, backlogCountHint and taskIDBlock).
func (h *Handler) DescribeTaskList(ctx context.Context, request *m.DescribeTaskListRequest) (resp *gen.DescribeTaskListResponse, retError error) {
	defer log.CapturePanic(h.GetLogger(), &retError)
	scope := metrics.MatchingDescribeTaskListScope
	sw := h.startRequestProfile("DescribeTaskList", scope)
	defer sw.Stop()

	if ok := h.rateLimiter.Allow(); !ok {
		return nil, h.handleErr(errMatchingHostThrottle, scope)
	}

	response, err := h.engine.DescribeTaskList(ctx, request)
	return response, h.handleErr(err, scope)
}

func (h *Handler) handleErr(err error, scope int) error {

	if err == nil {
		return nil
	}

	switch err.(type) {
	case *gen.InternalServiceError:
		h.metricsClient.IncCounter(scope, metrics.CadenceFailures)
		return err
	case *gen.BadRequestError:
		h.metricsClient.IncCounter(scope, metrics.CadenceErrBadRequestCounter)
		return err
	case *gen.EntityNotExistsError:
		h.metricsClient.IncCounter(scope, metrics.CadenceErrEntityNotExistsCounter)
		return err
	case *gen.WorkflowExecutionAlreadyStartedError:
		h.metricsClient.IncCounter(scope, metrics.CadenceErrExecutionAlreadyStartedCounter)
		return err
	case *gen.DomainAlreadyExistsError:
		h.metricsClient.IncCounter(scope, metrics.CadenceErrDomainAlreadyExistsCounter)
		return err
	case *gen.QueryFailedError:
		h.metricsClient.IncCounter(scope, metrics.CadenceErrQueryFailedCounter)
		return err
	case *gen.LimitExceededError:
		h.metricsClient.IncCounter(scope, metrics.CadenceErrLimitExceededCounter)
		return err
	case *gen.ServiceBusyError:
		h.metricsClient.IncCounter(scope, metrics.CadenceErrServiceBusyCounter)
		return err
	case *gen.DomainNotActiveError:
		h.metricsClient.IncCounter(scope, metrics.CadenceErrDomainNotActiveCounter)
		return err
	default:
		h.metricsClient.IncCounter(scope, metrics.CadenceFailures)
		return &gen.InternalServiceError{Message: err.Error()}
	}
}
