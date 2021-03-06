// The MIT License
//
// Copyright (c) 2020 Temporal Technologies Inc.  All rights reserved.
//
// Copyright (c) 2020 Uber Technologies, Inc.
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

package common

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dgryski/go-farm"
	"github.com/gogo/protobuf/proto"
	commonpb "go.temporal.io/temporal-proto/common/v1"
	enumspb "go.temporal.io/temporal-proto/enums/v1"
	historypb "go.temporal.io/temporal-proto/history/v1"
	"go.temporal.io/temporal-proto/serviceerror"
	"go.temporal.io/temporal-proto/workflowservice/v1"

	"github.com/temporalio/temporal/.gen/proto/historyservice/v1"
	"github.com/temporalio/temporal/.gen/proto/matchingservice/v1"
	"github.com/temporalio/temporal/common/backoff"
	"github.com/temporalio/temporal/common/log"
	"github.com/temporalio/temporal/common/log/tag"
	"github.com/temporalio/temporal/common/metrics"
	"github.com/temporalio/temporal/common/payload"
)

const (
	golandMapReserverNumberOfBytes = 48

	retryPersistenceOperationInitialInterval    = 50 * time.Millisecond
	retryPersistenceOperationMaxInterval        = 10 * time.Second
	retryPersistenceOperationExpirationInterval = 30 * time.Second

	historyServiceOperationInitialInterval    = 50 * time.Millisecond
	historyServiceOperationMaxInterval        = 10 * time.Second
	historyServiceOperationExpirationInterval = 30 * time.Second

	matchingServiceOperationInitialInterval    = 1000 * time.Millisecond
	matchingServiceOperationMaxInterval        = 10 * time.Second
	matchingServiceOperationExpirationInterval = 30 * time.Second

	frontendServiceOperationInitialInterval    = 200 * time.Millisecond
	frontendServiceOperationMaxInterval        = 5 * time.Second
	frontendServiceOperationExpirationInterval = 15 * time.Second

	adminServiceOperationInitialInterval    = 200 * time.Millisecond
	adminServiceOperationMaxInterval        = 5 * time.Second
	adminServiceOperationExpirationInterval = 15 * time.Second

	retryKafkaOperationInitialInterval    = 50 * time.Millisecond
	retryKafkaOperationMaxInterval        = 10 * time.Second
	retryKafkaOperationExpirationInterval = 30 * time.Second

	contextExpireThreshold = 10 * time.Millisecond

	// FailureReasonCompleteResultExceedsLimit is failureReason for complete result exceeds limit
	FailureReasonCompleteResultExceedsLimit = "Complete result exceeds size limit."
	// FailureReasonFailureDetailsExceedsLimit is failureReason for failure details exceeds limit
	FailureReasonFailureExceedsLimit = "Failure exceeds size limit."
	// FailureReasonCancelDetailsExceedsLimit is failureReason for cancel details exceeds limit
	FailureReasonCancelDetailsExceedsLimit = "Cancel details exceed size limit."
	// FailureReasonHeartbeatExceedsLimit is failureReason for heartbeat exceeds limit
	FailureReasonHeartbeatExceedsLimit = "Heartbeat details exceed size limit."
	// FailureReasonSizeExceedsLimit is reason to fail workflow when history size or count exceed limit
	FailureReasonSizeExceedsLimit = "Workflow history size / count exceeds limit."
	// FailureReasonTransactionSizeExceedsLimit is the failureReason for when transaction cannot be committed because it exceeds size limit
	FailureReasonTransactionSizeExceedsLimit = "Transaction size exceeds limit."
)

var (
	// ErrBlobSizeExceedsLimit is error for event blob size exceeds limit
	ErrBlobSizeExceedsLimit = serviceerror.NewInvalidArgument("Blob data size exceeds limit.")
	// ErrContextTimeoutTooShort is error for setting a very short context timeout when calling a long poll API
	ErrContextTimeoutTooShort = serviceerror.NewInvalidArgument("Context timeout is too short.")
	// ErrContextTimeoutNotSet is error for not setting a context timeout when calling a long poll API
	ErrContextTimeoutNotSet = serviceerror.NewInvalidArgument("Context timeout is not set.")
)

// AwaitWaitGroup calls Wait on the given wait
// Returns true if the Wait() call succeeded before the timeout
// Returns false if the Wait() did not return before the timeout
func AwaitWaitGroup(wg *sync.WaitGroup, timeout time.Duration) bool {

	doneC := make(chan struct{})

	go func() {
		wg.Wait()
		close(doneC)
	}()

	select {
	case <-doneC:
		return true
	case <-time.After(timeout):
		return false
	}
}

// AddSecondsToBaseTime - Gets the UnixNano with given duration and base time.
func AddSecondsToBaseTime(baseTimeInNanoSec int64, durationInSeconds int64) int64 {
	timeOut := time.Duration(durationInSeconds) * time.Second
	return time.Unix(0, baseTimeInNanoSec).Add(timeOut).UnixNano()
}

// CreatePersistanceRetryPolicy creates a retry policy for persistence layer operations
func CreatePersistanceRetryPolicy() backoff.RetryPolicy {
	policy := backoff.NewExponentialRetryPolicy(retryPersistenceOperationInitialInterval)
	policy.SetMaximumInterval(retryPersistenceOperationMaxInterval)
	policy.SetExpirationInterval(retryPersistenceOperationExpirationInterval)

	return policy
}

// CreateHistoryServiceRetryPolicy creates a retry policy for calls to history service
func CreateHistoryServiceRetryPolicy() backoff.RetryPolicy {
	policy := backoff.NewExponentialRetryPolicy(historyServiceOperationInitialInterval)
	policy.SetMaximumInterval(historyServiceOperationMaxInterval)
	policy.SetExpirationInterval(historyServiceOperationExpirationInterval)

	return policy
}

// CreateMatchingServiceRetryPolicy creates a retry policy for calls to matching service
func CreateMatchingServiceRetryPolicy() backoff.RetryPolicy {
	policy := backoff.NewExponentialRetryPolicy(matchingServiceOperationInitialInterval)
	policy.SetMaximumInterval(matchingServiceOperationMaxInterval)
	policy.SetExpirationInterval(matchingServiceOperationExpirationInterval)

	return policy
}

// CreateFrontendServiceRetryPolicy creates a retry policy for calls to frontend service
func CreateFrontendServiceRetryPolicy() backoff.RetryPolicy {
	policy := backoff.NewExponentialRetryPolicy(frontendServiceOperationInitialInterval)
	policy.SetMaximumInterval(frontendServiceOperationMaxInterval)
	policy.SetExpirationInterval(frontendServiceOperationExpirationInterval)

	return policy
}

// CreateAdminServiceRetryPolicy creates a retry policy for calls to matching service
func CreateAdminServiceRetryPolicy() backoff.RetryPolicy {
	policy := backoff.NewExponentialRetryPolicy(adminServiceOperationInitialInterval)
	policy.SetMaximumInterval(adminServiceOperationMaxInterval)
	policy.SetExpirationInterval(adminServiceOperationExpirationInterval)

	return policy
}

// CreateKafkaOperationRetryPolicy creates a retry policy for kafka operation
func CreateKafkaOperationRetryPolicy() backoff.RetryPolicy {
	policy := backoff.NewExponentialRetryPolicy(retryKafkaOperationInitialInterval)
	policy.SetMaximumInterval(retryKafkaOperationMaxInterval)
	policy.SetExpirationInterval(retryKafkaOperationExpirationInterval)

	return policy
}

// IsPersistenceTransientError checks if the error is a transient persistence error
func IsPersistenceTransientError(err error) bool {
	switch err.(type) {
	case *serviceerror.Internal,
		*serviceerror.ResourceExhausted:
		return true
	}

	return false
}

// IsKafkaTransientError check if the error is a transient kafka error
func IsKafkaTransientError(err error) bool {
	return true
}

// IsServiceTransientError checks if the error is a retryable error.
func IsServiceTransientError(err error) bool {
	return !IsServiceNonRetryableError(err)
}

// IsServiceNonRetryableError checks if the error is a non retryable error.
func IsServiceNonRetryableError(err error) bool {
	switch err.(type) {
	case *serviceerror.NotFound,
		*serviceerror.InvalidArgument,
		*serviceerror.NamespaceNotActive,
		*serviceerror.WorkflowExecutionAlreadyStarted,
		*serviceerror.CancellationAlreadyRequested:
		return true
	}

	return false
}

// IsWhitelistServiceTransientError checks if the error is a transient error.
func IsWhitelistServiceTransientError(err error) bool {
	if err == context.DeadlineExceeded {
		return true
	}

	switch err.(type) {
	case *serviceerror.Internal,
		*serviceerror.ResourceExhausted,
		*serviceerror.ShardOwnershipLost,
		*serviceerror.DeadlineExceeded,
		*serviceerror.Unavailable:
		return true
	}

	return false
}

// WorkflowIDToHistoryShard is used to map workflowID to a shardID
func WorkflowIDToHistoryShard(workflowID string, numberOfShards int) int {
	hash := farm.Fingerprint32([]byte(workflowID))
	return int(hash % uint32(numberOfShards))
}

// PrettyPrintHistory prints history in human readable format
func PrettyPrintHistory(history *historypb.History, logger log.Logger) {
	fmt.Println("******************************************")
	fmt.Println("History", proto.MarshalTextString(history))
	fmt.Println("******************************************")
}

// IsValidContext checks that the thrift context is not expired on cancelled.
// Returns nil if the context is still valid. Otherwise, returns the result of
// ctx.Err()
func IsValidContext(ctx context.Context) error {
	ch := ctx.Done()
	if ch != nil {
		select {
		case <-ch:
			return ctx.Err()
		default:
			return nil
		}
	}
	deadline, ok := ctx.Deadline()
	if ok && time.Until(deadline) < contextExpireThreshold {
		return context.DeadlineExceeded
	}
	return nil
}

// GenerateRandomString is used for generate test string
func GenerateRandomString(n int) string {
	rand.Seed(time.Now().UnixNano())
	letterRunes := []rune("random")
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

// CreateMatchingPollForDecisionTaskResponse create response for matching's PollForDecisionTask
func CreateMatchingPollForDecisionTaskResponse(historyResponse *historyservice.RecordDecisionTaskStartedResponse, workflowExecution *commonpb.WorkflowExecution, token []byte) *matchingservice.PollForDecisionTaskResponse {
	matchingResp := &matchingservice.PollForDecisionTaskResponse{
		TaskToken:                 token,
		WorkflowExecution:         workflowExecution,
		WorkflowType:              historyResponse.WorkflowType,
		PreviousStartedEventId:    historyResponse.PreviousStartedEventId,
		StartedEventId:            historyResponse.StartedEventId,
		Attempt:                   historyResponse.GetAttempt(),
		NextEventId:               historyResponse.NextEventId,
		StickyExecutionEnabled:    historyResponse.StickyExecutionEnabled,
		DecisionInfo:              historyResponse.DecisionInfo,
		WorkflowExecutionTaskList: historyResponse.WorkflowExecutionTaskList,
		EventStoreVersion:         historyResponse.EventStoreVersion,
		BranchToken:               historyResponse.BranchToken,
		ScheduledTimestamp:        historyResponse.ScheduledTimestamp,
		StartedTimestamp:          historyResponse.StartedTimestamp,
		Queries:                   historyResponse.Queries,
	}

	return matchingResp
}

// MinInt64 returns the smaller of two given int64
func MinInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// MaxInt64 returns the greater of two given int64
func MaxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// MinInt32 return smaller one of two inputs int32
func MinInt32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

// MinInt returns the smaller of two given integers
func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MaxInt returns the greater one of two given integers
func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// MinDuration returns the smaller of two given time duration
func MinDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// MaxDuration returns the greater of two given time durations
func MaxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// SortInt64Slice sorts the given int64 slice.
// Sort is not guaranteed to be stable.
func SortInt64Slice(slice []int64) {
	sort.Slice(slice, func(i int, j int) bool {
		return slice[i] < slice[j]
	})
}

// ValidateRetryPolicy validates a retry policy
func ValidateRetryPolicy(policy *commonpb.RetryPolicy) error {
	if policy == nil {
		// nil policy is valid which means no retry
		return nil
	}
	if policy.GetInitialIntervalInSeconds() < 0 {
		return serviceerror.NewInvalidArgument("InitialIntervalInSeconds cannot be negative on retry policy.")
	}
	if policy.GetBackoffCoefficient() < 1 {
		return serviceerror.NewInvalidArgument("BackoffCoefficient cannot be less than 1 on retry policy.")
	}
	if policy.GetMaximumIntervalInSeconds() < 0 {
		return serviceerror.NewInvalidArgument("MaximumIntervalInSeconds cannot be negative on retry policy.")
	}
	if policy.GetMaximumIntervalInSeconds() > 0 && policy.GetMaximumIntervalInSeconds() < policy.GetInitialIntervalInSeconds() {
		return serviceerror.NewInvalidArgument("MaximumIntervalInSeconds cannot be less than InitialIntervalInSeconds on retry policy.")
	}
	if policy.GetMaximumAttempts() < 0 {
		return serviceerror.NewInvalidArgument("MaximumAttempts cannot be negative on retry policy.")
	}
	return nil
}

// CreateHistoryStartWorkflowRequest create a start workflow request for history
func CreateHistoryStartWorkflowRequest(
	namespaceID string,
	startRequest *workflowservice.StartWorkflowExecutionRequest,
) *historyservice.StartWorkflowExecutionRequest {
	now := time.Now()
	histRequest := &historyservice.StartWorkflowExecutionRequest{
		NamespaceId:  namespaceID,
		StartRequest: startRequest,
		ContinueAsNewInitiator: enumspb.CONTINUE_AS_NEW_INITIATOR_DECIDER,
	}
	if startRequest.GetWorkflowExecutionTimeoutSeconds() > 0 {
		expirationInSeconds := startRequest.GetWorkflowExecutionTimeoutSeconds()
		deadline := now.Add(time.Second * time.Duration(expirationInSeconds))
		histRequest.WorkflowExecutionExpirationTimestamp = deadline.Round(time.Millisecond).UnixNano()
	}

	histRequest.FirstDecisionTaskBackoffSeconds = backoff.GetBackoffForNextScheduleInSeconds(startRequest.GetCronSchedule(), now, now)
	return histRequest
}

// CheckEventBlobSizeLimit checks if a blob data exceeds limits. It logs a warning if it exceeds warnLimit,
// and return ErrBlobSizeExceedsLimit if it exceeds errorLimit.
func CheckEventBlobSizeLimit(
	actualSize int,
	warnLimit int,
	errorLimit int,
	namespaceID string,
	workflowID string,
	runID string,
	scope metrics.Scope,
	logger log.Logger,
	blobSizeViolationOperationTag tag.Tag,
) error {
	scope.RecordTimer(metrics.EventBlobSize, time.Duration(actualSize))

	if actualSize > warnLimit {
		if logger != nil {
			logger.Warn("Blob size exceeds limit.",
				tag.WorkflowNamespaceID(namespaceID),
				tag.WorkflowID(workflowID),
				tag.WorkflowRunID(runID),
				tag.WorkflowSize(int64(actualSize)),
				blobSizeViolationOperationTag)
		}

		if actualSize > errorLimit {
			return ErrBlobSizeExceedsLimit
		}
	}
	return nil
}

// ValidateLongPollContextTimeout check if the context timeout for a long poll handler is too short or below a normal value.
// If the timeout is not set or too short, it logs an error, and return ErrContextTimeoutNotSet or ErrContextTimeoutTooShort
// accordingly. If the timeout is only below a normal value, it just logs an info and return nil.
func ValidateLongPollContextTimeout(
	ctx context.Context,
	handlerName string,
	logger log.Logger,
) error {

	deadline, err := ValidateLongPollContextTimeoutIsSet(ctx, handlerName, logger)
	if err != nil {
		return err
	}
	timeout := time.Until(deadline)
	if timeout < MinLongPollTimeout {
		err := ErrContextTimeoutTooShort
		logger.Error("Context timeout is too short for long poll API.",
			tag.WorkflowHandlerName(handlerName), tag.Error(err), tag.WorkflowPollContextTimeout(timeout))
		return err
	}
	if timeout < CriticalLongPollTimeout {
		logger.Warn("Context timeout is lower than critical value for long poll API.",
			tag.WorkflowHandlerName(handlerName), tag.WorkflowPollContextTimeout(timeout))
	}
	return nil
}

// ValidateLongPollContextTimeoutIsSet checks if the context timeout is set for long poll requests.
func ValidateLongPollContextTimeoutIsSet(
	ctx context.Context,
	handlerName string,
	logger log.Logger,
) (time.Time, error) {

	deadline, ok := ctx.Deadline()
	if !ok {
		err := ErrContextTimeoutNotSet
		logger.Error("Context timeout not set for long poll API.",
			tag.WorkflowHandlerName(handlerName), tag.Error(err))
		return deadline, err
	}
	return deadline, nil
}

// IsJustOrderByClause return true is query start with order by
func IsJustOrderByClause(clause string) bool {
	whereClause := strings.TrimSpace(clause)
	whereClause = strings.ToLower(whereClause)
	return strings.HasPrefix(whereClause, "order by")
}

// ConvertIndexedValueTypeToProtoType takes fieldType as interface{} and convert to IndexedValueType.
// Because different implementation of dynamic config client may lead to different types
func ConvertIndexedValueTypeToProtoType(fieldType interface{}, logger log.Logger) enumspb.IndexedValueType {
	switch t := fieldType.(type) {
	case float64:
		return enumspb.IndexedValueType(t)
	case int:
		return enumspb.IndexedValueType(t)
	case string:
		if ivt, ok := enumspb.IndexedValueType_value[t]; ok{
			return enumspb.IndexedValueType(ivt)
		}
	case enumspb.IndexedValueType:
		return t
	}

	// Unknown fieldType, please make sure dynamic config return correct value type
	logger.Error("unknown index value type", tag.Value(fieldType), tag.ValueType(fieldType))
	return fieldType.(enumspb.IndexedValueType) // it will panic and been captured by logger
}

// DeserializeSearchAttributeValue takes json encoded search attribute value and it's type as input, then
// unmarshal the value into a concrete type and return the value
func DeserializeSearchAttributeValue(value *commonpb.Payload, valueType enumspb.IndexedValueType) (interface{}, error) {
	switch valueType {
	case enumspb.INDEXED_VALUE_TYPE_STRING, enumspb.INDEXED_VALUE_TYPE_KEYWORD:
		var val string
		if err := payload.Decode(value, &val); err != nil {
			var listVal []string
			err = payload.Decode(value, &listVal)
			return listVal, err
		}
		return val, nil
	case enumspb.INDEXED_VALUE_TYPE_INT:
		var val int64
		if err := payload.Decode(value, &val); err != nil {
			var listVal []int64
			err = payload.Decode(value, &listVal)
			return listVal, err
		}
		return val, nil
	case enumspb.INDEXED_VALUE_TYPE_DOUBLE:
		var val float64
		if err := payload.Decode(value, &val); err != nil {
			var listVal []float64
			err = payload.Decode(value, &listVal)
			return listVal, err
		}
		return val, nil
	case enumspb.INDEXED_VALUE_TYPE_BOOL:
		var val bool
		if err := payload.Decode(value, &val); err != nil {
			var listVal []bool
			err = payload.Decode(value, &listVal)
			return listVal, err
		}
		return val, nil
	case enumspb.INDEXED_VALUE_TYPE_DATETIME:
		var val time.Time
		if err := payload.Decode(value, &val); err != nil {
			var listVal []time.Time
			err = payload.Decode(value, &listVal)
			return listVal, err
		}
		return val, nil
	default:
		return nil, fmt.Errorf("error: unknown index value type [%v]", valueType)
	}
}

// GetDefaultAdvancedVisibilityWritingMode get default advancedVisibilityWritingMode based on
// whether related config exists in static config file.
func GetDefaultAdvancedVisibilityWritingMode(isAdvancedVisConfigExist bool) string {
	if isAdvancedVisConfigExist {
		return AdvancedVisibilityWritingModeOn
	}
	return AdvancedVisibilityWritingModeOff
}

func GetPayloadsMapSize(data map[string]*commonpb.Payloads) int {
	size := 0
	for key, payloads := range data {
		size += len(key)
		size += payloads.Size()
	}

	return size
}
