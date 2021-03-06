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

package history

import (
	"fmt"
	"strings"
	"time"

	"github.com/pborman/uuid"
	commonpb "go.temporal.io/temporal-proto/common/v1"
	decisionpb "go.temporal.io/temporal-proto/decision/v1"
	enumspb "go.temporal.io/temporal-proto/enums/v1"
	"go.temporal.io/temporal-proto/serviceerror"
	tasklistpb "go.temporal.io/temporal-proto/tasklist/v1"

	"github.com/temporalio/temporal/common"
	"github.com/temporalio/temporal/common/backoff"
	"github.com/temporalio/temporal/common/cache"
	"github.com/temporalio/temporal/common/convert"
	"github.com/temporalio/temporal/common/elasticsearch/validator"
	"github.com/temporalio/temporal/common/failure"
	"github.com/temporalio/temporal/common/log"
	"github.com/temporalio/temporal/common/log/tag"
	"github.com/temporalio/temporal/common/metrics"
	"github.com/temporalio/temporal/common/persistence"
)

type (
	decisionAttrValidator struct {
		namespaceCache            cache.NamespaceCache
		maxIDLengthLimit          int
		searchAttributesValidator *validator.SearchAttributesValidator
	}

	workflowSizeChecker struct {
		blobSizeLimitWarn  int
		blobSizeLimitError int

		historySizeLimitWarn  int
		historySizeLimitError int

		historyCountLimitWarn  int
		historyCountLimitError int

		completedID    int64
		mutableState   mutableState
		executionStats *persistence.ExecutionStats
		metricsScope   metrics.Scope
		logger         log.Logger
	}
)

const (
	reservedTaskListPrefix = "/__temporal_sys/"
)

func newDecisionAttrValidator(
	namespaceCache cache.NamespaceCache,
	config *Config,
	logger log.Logger,
) *decisionAttrValidator {
	return &decisionAttrValidator{
		namespaceCache:   namespaceCache,
		maxIDLengthLimit: config.MaxIDLengthLimit(),
		searchAttributesValidator: validator.NewSearchAttributesValidator(
			logger,
			config.ValidSearchAttributes,
			config.SearchAttributesNumberOfKeysLimit,
			config.SearchAttributesSizeOfValueLimit,
			config.SearchAttributesTotalSizeLimit,
		),
	}
}

func newWorkflowSizeChecker(
	blobSizeLimitWarn int,
	blobSizeLimitError int,
	historySizeLimitWarn int,
	historySizeLimitError int,
	historyCountLimitWarn int,
	historyCountLimitError int,
	completedID int64,
	mutableState mutableState,
	executionStats *persistence.ExecutionStats,
	metricsScope metrics.Scope,
	logger log.Logger,
) *workflowSizeChecker {
	return &workflowSizeChecker{
		blobSizeLimitWarn:      blobSizeLimitWarn,
		blobSizeLimitError:     blobSizeLimitError,
		historySizeLimitWarn:   historySizeLimitWarn,
		historySizeLimitError:  historySizeLimitError,
		historyCountLimitWarn:  historyCountLimitWarn,
		historyCountLimitError: historyCountLimitError,
		completedID:            completedID,
		mutableState:           mutableState,
		executionStats:         executionStats,
		metricsScope:           metricsScope,
		logger:                 logger,
	}
}

func (c *workflowSizeChecker) failWorkflowIfPayloadSizeExceedsLimit(
	decisionTypeTag metrics.Tag,
	payloadSize int,
	message string,
) (bool, error) {

	executionInfo := c.mutableState.GetExecutionInfo()
	err := common.CheckEventBlobSizeLimit(
		payloadSize,
		c.blobSizeLimitWarn,
		c.blobSizeLimitError,
		executionInfo.NamespaceID,
		executionInfo.WorkflowID,
		executionInfo.RunID,
		c.metricsScope.Tagged(decisionTypeTag),
		c.logger,
		tag.BlobSizeViolationOperation(decisionTypeTag.Value()),
	)
	if err == nil {
		return false, nil
	}

	attributes := &decisionpb.FailWorkflowExecutionDecisionAttributes{
		Failure: failure.NewServerFailure(message, true),
	}

	if _, err := c.mutableState.AddFailWorkflowEvent(c.completedID, enumspb.RETRY_STATUS_NON_RETRYABLE_FAILURE, attributes); err != nil {
		return false, err
	}

	return true, nil
}

func (c *workflowSizeChecker) failWorkflowSizeExceedsLimit() (bool, error) {
	historyCount := int(c.mutableState.GetNextEventID()) - 1
	historySize := int(c.executionStats.HistorySize)

	if historySize > c.historySizeLimitError || historyCount > c.historyCountLimitError {
		executionInfo := c.mutableState.GetExecutionInfo()
		c.logger.Error("history size exceeds error limit.",
			tag.WorkflowNamespaceID(executionInfo.NamespaceID),
			tag.WorkflowID(executionInfo.WorkflowID),
			tag.WorkflowRunID(executionInfo.RunID),
			tag.WorkflowHistorySize(historySize),
			tag.WorkflowEventCount(historyCount))

		attributes := &decisionpb.FailWorkflowExecutionDecisionAttributes{
			Failure: failure.NewServerFailure(common.FailureReasonSizeExceedsLimit, false),
		}

		if _, err := c.mutableState.AddFailWorkflowEvent(c.completedID, enumspb.RETRY_STATUS_NON_RETRYABLE_FAILURE, attributes); err != nil {
			return false, err
		}
		return true, nil
	}

	if historySize > c.historySizeLimitWarn || historyCount > c.historyCountLimitWarn {
		executionInfo := c.mutableState.GetExecutionInfo()
		c.logger.Warn("history size exceeds warn limit.",
			tag.WorkflowNamespaceID(executionInfo.NamespaceID),
			tag.WorkflowID(executionInfo.WorkflowID),
			tag.WorkflowRunID(executionInfo.RunID),
			tag.WorkflowHistorySize(historySize),
			tag.WorkflowEventCount(historyCount))
		return false, nil
	}

	return false, nil
}

func (v *decisionAttrValidator) validateActivityScheduleAttributes(
	namespaceID string,
	targetNamespaceID string,
	attributes *decisionpb.ScheduleActivityTaskDecisionAttributes,
	runTimeout int32,
) error {

	if err := v.validateCrossNamespaceCall(
		namespaceID,
		targetNamespaceID,
	); err != nil {
		return err
	}

	if attributes == nil {
		return serviceerror.NewInvalidArgument("ScheduleActivityTaskDecisionAttributes is not set on decision.")
	}

	defaultTaskListName := ""
	if _, err := v.validatedTaskList(attributes.TaskList, defaultTaskListName); err != nil {
		return err
	}

	if attributes.GetActivityId() == "" {
		return serviceerror.NewInvalidArgument("ActivityId is not set on decision.")
	}

	if attributes.ActivityType == nil || attributes.ActivityType.GetName() == "" {
		return serviceerror.NewInvalidArgument("ActivityType is not set on decision.")
	}

	if err := common.ValidateRetryPolicy(attributes.RetryPolicy); err != nil {
		return err
	}

	if len(attributes.GetActivityId()) > v.maxIDLengthLimit {
		return serviceerror.NewInvalidArgument("ActivityID exceeds length limit.")
	}

	if len(attributes.GetActivityType().GetName()) > v.maxIDLengthLimit {
		return serviceerror.NewInvalidArgument("ActivityType exceeds length limit.")
	}

	if len(attributes.GetNamespace()) > v.maxIDLengthLimit {
		return serviceerror.NewInvalidArgument("Namespace exceeds length limit.")
	}

	// Only attempt to deduce and fill in unspecified timeouts only when all timeouts are non-negative.
	if attributes.GetScheduleToCloseTimeoutSeconds() < 0 || attributes.GetScheduleToStartTimeoutSeconds() < 0 ||
		attributes.GetStartToCloseTimeoutSeconds() < 0 || attributes.GetHeartbeatTimeoutSeconds() < 0 {
		return serviceerror.NewInvalidArgument("A valid timeout may not be negative.")
	}

	validScheduleToClose := attributes.GetScheduleToCloseTimeoutSeconds() > 0
	validScheduleToStart := attributes.GetScheduleToStartTimeoutSeconds() > 0
	validStartToClose := attributes.GetStartToCloseTimeoutSeconds() > 0

	if validScheduleToClose {
		if validScheduleToStart {
			attributes.ScheduleToStartTimeoutSeconds = common.MinInt32(attributes.GetScheduleToStartTimeoutSeconds(),
				attributes.GetScheduleToCloseTimeoutSeconds())
		} else {
			attributes.ScheduleToStartTimeoutSeconds = attributes.GetScheduleToCloseTimeoutSeconds()
		}
		if validStartToClose {
			attributes.StartToCloseTimeoutSeconds = common.MinInt32(attributes.GetStartToCloseTimeoutSeconds(),
				attributes.GetScheduleToCloseTimeoutSeconds())
		} else {
			attributes.StartToCloseTimeoutSeconds = attributes.GetScheduleToCloseTimeoutSeconds()
		}
	} else if validStartToClose {
		// We are in !validScheduleToClose due to the first if above
		attributes.ScheduleToCloseTimeoutSeconds = runTimeout
		if !validScheduleToStart {
			attributes.ScheduleToStartTimeoutSeconds = runTimeout
		}
	} else {
		// Deduction failed as there's not enough information to fill in missing timeouts.
		return serviceerror.NewInvalidArgument("A valid StartToClose or ScheduleToCloseTimeout is not set on decision.")
	}
	// ensure activity timeout never larger than workflow timeout
	if runTimeout > 0 {
		if attributes.GetScheduleToCloseTimeoutSeconds() > runTimeout {
			attributes.ScheduleToCloseTimeoutSeconds = runTimeout
		}
		if attributes.GetScheduleToStartTimeoutSeconds() > runTimeout {
			attributes.ScheduleToStartTimeoutSeconds = runTimeout
		}
		if attributes.GetStartToCloseTimeoutSeconds() > runTimeout {
			attributes.StartToCloseTimeoutSeconds = runTimeout
		}
		if attributes.GetHeartbeatTimeoutSeconds() > runTimeout {
			attributes.HeartbeatTimeoutSeconds = runTimeout
		}
	}
	if attributes.GetHeartbeatTimeoutSeconds() > attributes.GetScheduleToCloseTimeoutSeconds() {
		attributes.HeartbeatTimeoutSeconds = attributes.GetScheduleToCloseTimeoutSeconds()
	}
	return nil
}

func (v *decisionAttrValidator) validateTimerScheduleAttributes(
	attributes *decisionpb.StartTimerDecisionAttributes,
) error {

	if attributes == nil {
		return serviceerror.NewInvalidArgument("StartTimerDecisionAttributes is not set on decision.")
	}
	if attributes.GetTimerId() == "" {
		return serviceerror.NewInvalidArgument("TimerId is not set on decision.")
	}
	if len(attributes.GetTimerId()) > v.maxIDLengthLimit {
		return serviceerror.NewInvalidArgument("TimerId exceeds length limit.")
	}
	if attributes.GetStartToFireTimeoutSeconds() <= 0 {
		return serviceerror.NewInvalidArgument("A valid StartToFireTimeoutSeconds is not set on decision.")
	}
	return nil
}

func (v *decisionAttrValidator) validateActivityCancelAttributes(
	attributes *decisionpb.RequestCancelActivityTaskDecisionAttributes,
) error {

	if attributes == nil {
		return serviceerror.NewInvalidArgument("RequestCancelActivityTaskDecisionAttributes is not set on decision.")
	}
	if attributes.GetScheduledEventId() <= 0 {
		return serviceerror.NewInvalidArgument("ScheduledEventId is not set on decision.")
	}
	return nil
}

func (v *decisionAttrValidator) validateTimerCancelAttributes(
	attributes *decisionpb.CancelTimerDecisionAttributes,
) error {

	if attributes == nil {
		return serviceerror.NewInvalidArgument("CancelTimerDecisionAttributes is not set on decision.")
	}
	if attributes.GetTimerId() == "" {
		return serviceerror.NewInvalidArgument("TimerId is not set on decision.")
	}
	if len(attributes.GetTimerId()) > v.maxIDLengthLimit {
		return serviceerror.NewInvalidArgument("TimerId exceeds length limit.")
	}
	return nil
}

func (v *decisionAttrValidator) validateRecordMarkerAttributes(
	attributes *decisionpb.RecordMarkerDecisionAttributes,
) error {

	if attributes == nil {
		return serviceerror.NewInvalidArgument("RecordMarkerDecisionAttributes is not set on decision.")
	}
	if attributes.GetMarkerName() == "" {
		return serviceerror.NewInvalidArgument("MarkerName is not set on decision.")
	}
	if len(attributes.GetMarkerName()) > v.maxIDLengthLimit {
		return serviceerror.NewInvalidArgument("MarkerName exceeds length limit.")
	}

	return nil
}

func (v *decisionAttrValidator) validateCompleteWorkflowExecutionAttributes(
	attributes *decisionpb.CompleteWorkflowExecutionDecisionAttributes,
) error {

	if attributes == nil {
		return serviceerror.NewInvalidArgument("CompleteWorkflowExecutionDecisionAttributes is not set on decision.")
	}
	return nil
}

func (v *decisionAttrValidator) validateFailWorkflowExecutionAttributes(
	attributes *decisionpb.FailWorkflowExecutionDecisionAttributes,
) error {

	if attributes == nil {
		return serviceerror.NewInvalidArgument("FailWorkflowExecutionDecisionAttributes is not set on decision.")
	}
	if attributes.GetFailure() == nil {
		return serviceerror.NewInvalidArgument("Failure is not set on decision.")
	}
	return nil
}

func (v *decisionAttrValidator) validateCancelWorkflowExecutionAttributes(
	attributes *decisionpb.CancelWorkflowExecutionDecisionAttributes,
) error {

	if attributes == nil {
		return serviceerror.NewInvalidArgument("CancelWorkflowExecutionDecisionAttributes is not set on decision.")
	}
	return nil
}

func (v *decisionAttrValidator) validateCancelExternalWorkflowExecutionAttributes(
	namespaceID string,
	targetNamespaceID string,
	attributes *decisionpb.RequestCancelExternalWorkflowExecutionDecisionAttributes,
) error {

	if err := v.validateCrossNamespaceCall(
		namespaceID,
		targetNamespaceID,
	); err != nil {
		return err
	}

	if attributes == nil {
		return serviceerror.NewInvalidArgument("RequestCancelExternalWorkflowExecutionDecisionAttributes is not set on decision.")
	}
	if attributes.GetWorkflowId() == "" {
		return serviceerror.NewInvalidArgument("WorkflowId is not set on decision.")
	}
	if len(attributes.GetNamespace()) > v.maxIDLengthLimit {
		return serviceerror.NewInvalidArgument("Namespace exceeds length limit.")
	}
	if len(attributes.GetWorkflowId()) > v.maxIDLengthLimit {
		return serviceerror.NewInvalidArgument("WorkflowId exceeds length limit.")
	}
	runID := attributes.GetRunId()
	if runID != "" && uuid.Parse(runID) == nil {
		return serviceerror.NewInvalidArgument("Invalid RunId set on decision.")
	}

	return nil
}

func (v *decisionAttrValidator) validateSignalExternalWorkflowExecutionAttributes(
	namespaceID string,
	targetNamespaceID string,
	attributes *decisionpb.SignalExternalWorkflowExecutionDecisionAttributes,
) error {

	if err := v.validateCrossNamespaceCall(
		namespaceID,
		targetNamespaceID,
	); err != nil {
		return err
	}

	if attributes == nil {
		return serviceerror.NewInvalidArgument("SignalExternalWorkflowExecutionDecisionAttributes is not set on decision.")
	}
	if attributes.Execution == nil {
		return serviceerror.NewInvalidArgument("Execution is nil on decision.")
	}
	if attributes.Execution.GetWorkflowId() == "" {
		return serviceerror.NewInvalidArgument("WorkflowId is not set on decision.")
	}
	if len(attributes.GetNamespace()) > v.maxIDLengthLimit {
		return serviceerror.NewInvalidArgument("Namespace exceeds length limit.")
	}
	if len(attributes.Execution.GetWorkflowId()) > v.maxIDLengthLimit {
		return serviceerror.NewInvalidArgument("WorkflowId exceeds length limit.")
	}

	targetRunID := attributes.Execution.GetRunId()
	if targetRunID != "" && uuid.Parse(targetRunID) == nil {
		return serviceerror.NewInvalidArgument("Invalid RunId set on decision.")
	}
	if attributes.GetSignalName() == "" {
		return serviceerror.NewInvalidArgument("SignalName is not set on decision.")
	}

	return nil
}

func (v *decisionAttrValidator) validateUpsertWorkflowSearchAttributes(
	namespace string,
	attributes *decisionpb.UpsertWorkflowSearchAttributesDecisionAttributes,
) error {

	if attributes == nil {
		return serviceerror.NewInvalidArgument("UpsertWorkflowSearchAttributesDecisionAttributes is not set on decision.")
	}

	if attributes.SearchAttributes == nil {
		return serviceerror.NewInvalidArgument("SearchAttributes is not set on decision.")
	}

	if len(attributes.GetSearchAttributes().GetIndexedFields()) == 0 {
		return serviceerror.NewInvalidArgument("IndexedFields is empty on decision.")
	}

	return v.searchAttributesValidator.ValidateSearchAttributes(attributes.GetSearchAttributes(), namespace)
}

func (v *decisionAttrValidator) validateContinueAsNewWorkflowExecutionAttributes(
	attributes *decisionpb.ContinueAsNewWorkflowExecutionDecisionAttributes,
	executionInfo *persistence.WorkflowExecutionInfo,
) error {

	if attributes == nil {
		return serviceerror.NewInvalidArgument("ContinueAsNewWorkflowExecutionDecisionAttributes is not set on decision.")
	}

	// Inherit workflow type from previous execution if not provided on decision
	if attributes.WorkflowType == nil || attributes.WorkflowType.GetName() == "" {
		attributes.WorkflowType = &commonpb.WorkflowType{Name: executionInfo.WorkflowTypeName}
	}

	if len(attributes.WorkflowType.GetName()) > v.maxIDLengthLimit {
		return serviceerror.NewInvalidArgument("WorkflowType exceeds length limit.")
	}

	// Inherit Tasklist from previous execution if not provided on decision
	taskList, err := v.validatedTaskList(attributes.TaskList, executionInfo.TaskList)
	if err != nil {
		return err
	}
	attributes.TaskList = taskList

	// Reduce runTimeout if it is going to exceed WorkflowExpirationTime
	// Note that this calculation can produce negative result
	// handleDecisionContinueAsNewWorkflow must handle negative runTimeout value
	timeoutTime := executionInfo.WorkflowExpirationTime
	if !timeoutTime.IsZero() {
		runTimeout := convert.Int32Ceil(timeoutTime.Sub(time.Now()).Seconds())
		if attributes.GetWorkflowRunTimeoutSeconds() > 0 {
			runTimeout = common.MinInt32(runTimeout, attributes.GetWorkflowRunTimeoutSeconds())
		} else {
			runTimeout = common.MinInt32(runTimeout, executionInfo.WorkflowRunTimeout)
		}
		attributes.WorkflowRunTimeoutSeconds = runTimeout
	} else if attributes.GetWorkflowRunTimeoutSeconds() == 0 {
		attributes.WorkflowRunTimeoutSeconds = executionInfo.WorkflowRunTimeout
	}

	// Inherit decision task timeout from previous execution if not provided on decision
	if attributes.GetWorkflowTaskTimeoutSeconds() <= 0 {
		attributes.WorkflowTaskTimeoutSeconds = executionInfo.WorkflowTaskTimeout
	}

	// Check next run decision task delay
	if attributes.GetBackoffStartIntervalInSeconds() < 0 {
		return serviceerror.NewInvalidArgument("BackoffStartInterval is less than 0.")
	}

	namespaceEntry, err := v.namespaceCache.GetNamespaceByID(executionInfo.NamespaceID)
	if err != nil {
		return err
	}
	return v.searchAttributesValidator.ValidateSearchAttributes(attributes.GetSearchAttributes(), namespaceEntry.GetInfo().Name)
}

func (v *decisionAttrValidator) validateStartChildExecutionAttributes(
	namespaceID string,
	targetNamespaceID string,
	attributes *decisionpb.StartChildWorkflowExecutionDecisionAttributes,
	parentInfo *persistence.WorkflowExecutionInfo,
) error {

	if err := v.validateCrossNamespaceCall(
		namespaceID,
		targetNamespaceID,
	); err != nil {
		return err
	}

	if attributes == nil {
		return serviceerror.NewInvalidArgument("StartChildWorkflowExecutionDecisionAttributes is not set on decision.")
	}

	if attributes.GetWorkflowId() == "" {
		return serviceerror.NewInvalidArgument("Required field WorkflowId is not set on decision.")
	}

	if attributes.WorkflowType == nil || attributes.WorkflowType.GetName() == "" {
		return serviceerror.NewInvalidArgument("Required field WorkflowType is not set on decision.")
	}

	if len(attributes.GetNamespace()) > v.maxIDLengthLimit {
		return serviceerror.NewInvalidArgument("Namespace exceeds length limit.")
	}

	if len(attributes.GetWorkflowId()) > v.maxIDLengthLimit {
		return serviceerror.NewInvalidArgument("WorkflowId exceeds length limit.")
	}

	if len(attributes.WorkflowType.GetName()) > v.maxIDLengthLimit {
		return serviceerror.NewInvalidArgument("WorkflowType exceeds length limit.")
	}

	if err := common.ValidateRetryPolicy(attributes.RetryPolicy); err != nil {
		return err
	}

	if err := backoff.ValidateSchedule(attributes.GetCronSchedule()); err != nil {
		return err
	}

	// Inherit tasklist from parent workflow execution if not provided on decision
	taskList, err := v.validatedTaskList(attributes.TaskList, parentInfo.TaskList)
	if err != nil {
		return err
	}
	attributes.TaskList = taskList

	// Inherit workflow timeout from parent workflow execution if not provided on decision
	if attributes.GetWorkflowExecutionTimeoutSeconds() <= 0 {
		attributes.WorkflowExecutionTimeoutSeconds = parentInfo.WorkflowExecutionTimeout
	}

	// Inherit workflow timeout from parent workflow execution if not provided on decision
	if attributes.GetWorkflowRunTimeoutSeconds() <= 0 {
		attributes.WorkflowRunTimeoutSeconds = parentInfo.WorkflowRunTimeout
	}

	// Inherit decision task timeout from parent workflow execution if not provided on decision
	if attributes.GetWorkflowTaskTimeoutSeconds() <= 0 {
		attributes.WorkflowTaskTimeoutSeconds = parentInfo.WorkflowTaskTimeout
	}

	return nil
}

func (v *decisionAttrValidator) validatedTaskList(
	taskList *tasklistpb.TaskList,
	defaultVal string,
) (*tasklistpb.TaskList, error) {

	if taskList == nil {
		taskList = &tasklistpb.TaskList{}
	}

	if taskList.GetName() == "" {
		if defaultVal == "" {
			return taskList, serviceerror.NewInvalidArgument("missing task list name")
		}
		taskList.Name = defaultVal
		return taskList, nil
	}

	name := taskList.GetName()
	if len(name) > v.maxIDLengthLimit {
		return taskList, serviceerror.NewInvalidArgument(fmt.Sprintf("task list name exceeds length limit of %v", v.maxIDLengthLimit))
	}

	if strings.HasPrefix(name, reservedTaskListPrefix) {
		return taskList, serviceerror.NewInvalidArgument(fmt.Sprintf("task list name cannot start with reserved prefix %v", reservedTaskListPrefix))
	}

	return taskList, nil
}

func (v *decisionAttrValidator) validateCrossNamespaceCall(
	namespaceID string,
	targetNamespaceID string,
) error {

	// same name, no check needed
	if namespaceID == targetNamespaceID {
		return nil
	}

	namespaceEntry, err := v.namespaceCache.GetNamespaceByID(namespaceID)
	if err != nil {
		return err
	}

	targetNamespaceEntry, err := v.namespaceCache.GetNamespaceByID(targetNamespaceID)
	if err != nil {
		return err
	}

	// both local namespace
	if !namespaceEntry.IsGlobalNamespace() && !targetNamespaceEntry.IsGlobalNamespace() {
		return nil
	}

	namespaceClusters := namespaceEntry.GetReplicationConfig().Clusters
	targetNamespaceClusters := targetNamespaceEntry.GetReplicationConfig().Clusters

	// one is local namespace, another one is global namespace or both global namespace
	// treat global namespace with one replication cluster as local namespace
	if len(namespaceClusters) == 1 && len(targetNamespaceClusters) == 1 {
		if namespaceClusters[0] == targetNamespaceClusters[0] {
			return nil
		}
		return v.createCrossNamespaceCallError(namespaceEntry, targetNamespaceEntry)
	}
	return v.createCrossNamespaceCallError(namespaceEntry, targetNamespaceEntry)
}

func (v *decisionAttrValidator) createCrossNamespaceCallError(
	namespaceEntry *cache.NamespaceCacheEntry,
	targetNamespaceEntry *cache.NamespaceCacheEntry,
) error {
	return serviceerror.NewInvalidArgument(fmt.Sprintf("cannot make cross namespace call between %v and %v", namespaceEntry.GetInfo().Name, targetNamespaceEntry.GetInfo().Name))
}
