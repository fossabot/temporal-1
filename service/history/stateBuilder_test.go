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
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/golang/mock/gomock"
	"github.com/pborman/uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	commonpb "go.temporal.io/temporal-proto/common/v1"
	enumspb "go.temporal.io/temporal-proto/enums/v1"
	historypb "go.temporal.io/temporal-proto/history/v1"
	tasklistpb "go.temporal.io/temporal-proto/tasklist/v1"

	"github.com/temporalio/temporal/.gen/proto/persistenceblobs/v1"
	"github.com/temporalio/temporal/common"
	"github.com/temporalio/temporal/common/backoff"
	"github.com/temporalio/temporal/common/cache"
	"github.com/temporalio/temporal/common/cluster"
	"github.com/temporalio/temporal/common/log"
	"github.com/temporalio/temporal/common/payloads"
	"github.com/temporalio/temporal/common/persistence"
)

type (
	stateBuilderSuite struct {
		suite.Suite
		*require.Assertions

		controller          *gomock.Controller
		mockShard           *shardContextTest
		mockEventsCache     *MockeventsCache
		mockNamespaceCache  *cache.MockNamespaceCache
		mockTaskGenerator   *MockmutableStateTaskGenerator
		mockMutableState    *MockmutableState
		mockClusterMetadata *cluster.MockMetadata

		mockTaskGeneratorForNew *MockmutableStateTaskGenerator

		logger log.Logger

		sourceCluster string
		stateBuilder  *stateBuilderImpl
	}
)

func TestStateBuilderSuite(t *testing.T) {
	s := new(stateBuilderSuite)
	suite.Run(t, s)
}

func (s *stateBuilderSuite) SetupSuite() {

}

func (s *stateBuilderSuite) TearDownSuite() {

}

func (s *stateBuilderSuite) SetupTest() {
	s.Assertions = require.New(s.T())

	s.controller = gomock.NewController(s.T())
	s.mockTaskGenerator = NewMockmutableStateTaskGenerator(s.controller)
	s.mockMutableState = NewMockmutableState(s.controller)
	s.mockTaskGeneratorForNew = NewMockmutableStateTaskGenerator(s.controller)

	s.mockShard = newTestShardContext(
		s.controller,
		&persistence.ShardInfoWithFailover{
			ShardInfo: &persistenceblobs.ShardInfo{
				ShardId:          0,
				RangeId:          1,
				TransferAckLevel: 0,
			}},
		NewDynamicConfigForTest(),
	)

	s.mockNamespaceCache = s.mockShard.resource.NamespaceCache
	s.mockClusterMetadata = s.mockShard.resource.ClusterMetadata
	s.mockEventsCache = s.mockShard.mockEventsCache
	s.mockClusterMetadata.EXPECT().GetCurrentClusterName().Return(cluster.TestCurrentClusterName).AnyTimes()
	s.mockClusterMetadata.EXPECT().IsGlobalNamespaceEnabled().Return(true).AnyTimes()
	s.mockEventsCache.EXPECT().putEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	s.logger = s.mockShard.GetLogger()

	s.mockMutableState.EXPECT().GetReplicationState().Return(&persistence.ReplicationState{}).AnyTimes()
	s.stateBuilder = newStateBuilder(
		s.mockShard,
		s.logger,
		s.mockMutableState,
		func(mutableState mutableState) mutableStateTaskGenerator {
			if mutableState == s.mockMutableState {
				return s.mockTaskGenerator
			}
			return s.mockTaskGeneratorForNew
		},
	)
	s.sourceCluster = "some random source cluster"
}

func (s *stateBuilderSuite) TearDownTest() {
	s.stateBuilder = nil
	s.controller.Finish()
	s.mockShard.Finish(s.T())
}

func (s *stateBuilderSuite) mockUpdateVersion(events ...*historypb.HistoryEvent) {
	for _, event := range events {
		s.mockMutableState.EXPECT().UpdateReplicationStateVersion(event.GetVersion(), true).Times(1)
		s.mockMutableState.EXPECT().UpdateReplicationStateLastEventID(event.GetVersion(), event.GetEventId()).Times(1)
	}
	s.mockTaskGenerator.EXPECT().generateActivityTimerTasks(
		s.stateBuilder.unixNanoToTime(events[len(events)-1].GetTimestamp()),
	).Return(nil).Times(1)
	s.mockTaskGenerator.EXPECT().generateUserTimerTasks(
		s.stateBuilder.unixNanoToTime(events[len(events)-1].GetTimestamp()),
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().SetHistoryBuilder(newHistoryBuilderFromEvents(events, s.logger)).Times(1)
}

func (s *stateBuilderSuite) toHistory(events ...*historypb.HistoryEvent) []*historypb.HistoryEvent {
	return events
}

// workflow operations

func (s *stateBuilderSuite) TestApplyEvents_EventTypeWorkflowExecutionStarted_NoCronSchedule() {
	cronSchedule := ""
	version := int64(1)
	requestID := uuid.New()
	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	executionInfo := &persistence.WorkflowExecutionInfo{
		WorkflowRunTimeout: 100,
		CronSchedule:       cronSchedule,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_STARTED
	startWorkflowAttribute := &historypb.WorkflowExecutionStartedEventAttributes{
		ParentWorkflowNamespace: testParentNamespace,
	}

	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    1,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionStartedEventAttributes{WorkflowExecutionStartedEventAttributes: startWorkflowAttribute},
	}

	s.mockNamespaceCache.EXPECT().GetNamespace(testParentNamespace).Return(testGlobalParentNamespaceEntry, nil).Times(1)
	s.mockMutableState.EXPECT().ReplicateWorkflowExecutionStartedEvent(testParentNamespaceID, execution, requestID, event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(executionInfo).AnyTimes()
	s.mockTaskGenerator.EXPECT().generateRecordWorkflowStartedTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
		event,
	).Return(nil).Times(1)
	s.mockTaskGenerator.EXPECT().generateWorkflowStartTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
		event,
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)
	s.mockMutableState.EXPECT().SetHistoryTree(testRunID).Return(nil).Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeWorkflowExecutionStarted_WithCronSchedule() {
	cronSchedule := "* * * * *"
	version := int64(1)
	requestID := uuid.New()
	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	executionInfo := &persistence.WorkflowExecutionInfo{
		WorkflowRunTimeout: 100,
		CronSchedule:       cronSchedule,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_STARTED
	startWorkflowAttribute := &historypb.WorkflowExecutionStartedEventAttributes{
		ParentWorkflowNamespace:         testParentNamespace,
		Initiator:                       enumspb.CONTINUE_AS_NEW_INITIATOR_CRON_SCHEDULE,
		FirstDecisionTaskBackoffSeconds: int32(backoff.GetBackoffForNextSchedule(cronSchedule, now, now).Seconds()),
	}

	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    1,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionStartedEventAttributes{WorkflowExecutionStartedEventAttributes: startWorkflowAttribute},
	}

	s.mockNamespaceCache.EXPECT().GetNamespace(testParentNamespace).Return(testGlobalParentNamespaceEntry, nil).Times(1)
	s.mockMutableState.EXPECT().ReplicateWorkflowExecutionStartedEvent(testParentNamespaceID, execution, requestID, event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(executionInfo).AnyTimes()
	s.mockTaskGenerator.EXPECT().generateRecordWorkflowStartedTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
		event,
	).Return(nil).Times(1)
	s.mockTaskGenerator.EXPECT().generateWorkflowStartTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
		event,
	).Return(nil).Times(1)
	s.mockTaskGenerator.EXPECT().generateDelayedDecisionTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
		event,
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)
	s.mockMutableState.EXPECT().SetHistoryTree(testRunID).Return(nil).Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeWorkflowExecutionTimedOut() {
	version := int64(1)
	requestID := uuid.New()
	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_TIMED_OUT
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionTimedOutEventAttributes{WorkflowExecutionTimedOutEventAttributes: &historypb.WorkflowExecutionTimedOutEventAttributes{}},
	}

	s.mockMutableState.EXPECT().ReplicateWorkflowExecutionTimedoutEvent(event.GetEventId(), event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockTaskGenerator.EXPECT().generateWorkflowCloseTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeWorkflowExecutionTerminated() {
	version := int64(1)
	requestID := uuid.New()
	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_TERMINATED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionTerminatedEventAttributes{WorkflowExecutionTerminatedEventAttributes: &historypb.WorkflowExecutionTerminatedEventAttributes{}},
	}

	s.mockMutableState.EXPECT().ReplicateWorkflowExecutionTerminatedEvent(event.GetEventId(), event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockTaskGenerator.EXPECT().generateWorkflowCloseTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)
	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeWorkflowExecutionFailed() {
	version := int64(1)
	requestID := uuid.New()
	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_FAILED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionFailedEventAttributes{WorkflowExecutionFailedEventAttributes: &historypb.WorkflowExecutionFailedEventAttributes{}},
	}

	s.mockMutableState.EXPECT().ReplicateWorkflowExecutionFailedEvent(event.GetEventId(), event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockTaskGenerator.EXPECT().generateWorkflowCloseTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeWorkflowExecutionCompleted() {
	version := int64(1)
	requestID := uuid.New()
	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_COMPLETED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionCompletedEventAttributes{WorkflowExecutionCompletedEventAttributes: &historypb.WorkflowExecutionCompletedEventAttributes{}},
	}

	s.mockMutableState.EXPECT().ReplicateWorkflowExecutionCompletedEvent(event.GetEventId(), event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockTaskGenerator.EXPECT().generateWorkflowCloseTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeWorkflowExecutionCanceled() {
	version := int64(1)
	requestID := uuid.New()
	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_CANCELED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionCanceledEventAttributes{WorkflowExecutionCanceledEventAttributes: &historypb.WorkflowExecutionCanceledEventAttributes{}},
	}

	s.mockMutableState.EXPECT().ReplicateWorkflowExecutionCanceledEvent(event.GetEventId(), event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockTaskGenerator.EXPECT().generateWorkflowCloseTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeWorkflowExecutionContinuedAsNew() {
	version := int64(1)
	requestID := uuid.New()
	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}
	parentWorkflowID := "some random parent workflow ID"
	parentRunID := uuid.New()
	parentInitiatedEventID := int64(144)

	now := time.Now()
	tasklist := "some random tasklist"
	workflowType := "some random workflow type"
	workflowTimeoutSecond := int32(110)
	taskTimeoutSeconds := int32(11)
	newRunID := uuid.New()

	continueAsNewEvent := &historypb.HistoryEvent{
		Version:   version,
		EventId:   130,
		Timestamp: now.UnixNano(),
		EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_CONTINUED_AS_NEW,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionContinuedAsNewEventAttributes{WorkflowExecutionContinuedAsNewEventAttributes: &historypb.WorkflowExecutionContinuedAsNewEventAttributes{
			NewExecutionRunId: newRunID,
		}},
	}

	newRunStartedEvent := &historypb.HistoryEvent{
		Version:   version,
		EventId:   1,
		Timestamp: now.UnixNano(),
		EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_STARTED,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionStartedEventAttributes{WorkflowExecutionStartedEventAttributes: &historypb.WorkflowExecutionStartedEventAttributes{
			ParentWorkflowNamespace: testParentNamespace,
			ParentWorkflowExecution: &commonpb.WorkflowExecution{
				WorkflowId: parentWorkflowID,
				RunId:      parentRunID,
			},
			ParentInitiatedEventId:          parentInitiatedEventID,
			WorkflowExecutionTimeoutSeconds: workflowTimeoutSecond,
			WorkflowTaskTimeoutSeconds:      taskTimeoutSeconds,
			TaskList:                        &tasklistpb.TaskList{Name: tasklist},
			WorkflowType:                    &commonpb.WorkflowType{Name: workflowType},
		}},
	}

	newRunSignalEvent := &historypb.HistoryEvent{
		Version:   version,
		EventId:   2,
		Timestamp: now.UnixNano(),
		EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_SIGNALED,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionSignaledEventAttributes{WorkflowExecutionSignaledEventAttributes: &historypb.WorkflowExecutionSignaledEventAttributes{
			SignalName: "some random signal name",
			Input:      payloads.EncodeString("some random signal input"),
			Identity:   "some random identity",
		}},
	}

	newRunDecisionAttempt := int64(123)
	newRunDecisionEvent := &historypb.HistoryEvent{
		Version:   version,
		EventId:   3,
		Timestamp: now.UnixNano(),
		EventType: enumspb.EVENT_TYPE_DECISION_TASK_SCHEDULED,
		Attributes: &historypb.HistoryEvent_DecisionTaskScheduledEventAttributes{DecisionTaskScheduledEventAttributes: &historypb.DecisionTaskScheduledEventAttributes{
			TaskList:                   &tasklistpb.TaskList{Name: tasklist},
			StartToCloseTimeoutSeconds: taskTimeoutSeconds,
			Attempt:                    newRunDecisionAttempt,
		}},
	}
	newRunEvents := []*historypb.HistoryEvent{
		newRunStartedEvent, newRunSignalEvent, newRunDecisionEvent,
	}

	s.mockClusterMetadata.EXPECT().ClusterNameForFailoverVersion(continueAsNewEvent.GetVersion()).Return(s.sourceCluster).AnyTimes()
	s.mockMutableState.EXPECT().ReplicateWorkflowExecutionContinuedAsNewEvent(
		continueAsNewEvent.GetEventId(),
		testNamespaceID,
		continueAsNewEvent,
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().GetNamespaceEntry().Return(testGlobalNamespaceEntry).AnyTimes()
	s.mockUpdateVersion(continueAsNewEvent)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockTaskGenerator.EXPECT().generateWorkflowCloseTasks(
		s.stateBuilder.unixNanoToTime(continueAsNewEvent.GetTimestamp()),
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	// new workflow namespace
	s.mockNamespaceCache.EXPECT().GetNamespace(testParentNamespace).Return(testGlobalParentNamespaceEntry, nil).AnyTimes()
	// task for the new workflow
	s.mockTaskGeneratorForNew.EXPECT().generateRecordWorkflowStartedTasks(
		s.stateBuilder.unixNanoToTime(newRunStartedEvent.GetTimestamp()),
		newRunStartedEvent,
	).Return(nil).Times(1)
	s.mockTaskGeneratorForNew.EXPECT().generateWorkflowStartTasks(
		s.stateBuilder.unixNanoToTime(newRunStartedEvent.GetTimestamp()),
		newRunStartedEvent,
	).Return(nil).Times(1)
	s.mockTaskGeneratorForNew.EXPECT().generateDecisionScheduleTasks(
		s.stateBuilder.unixNanoToTime(newRunDecisionEvent.GetTimestamp()),
		newRunDecisionEvent.GetEventId(),
	).Return(nil).Times(1)
	s.mockTaskGeneratorForNew.EXPECT().generateActivityTimerTasks(
		s.stateBuilder.unixNanoToTime(newRunEvents[len(newRunEvents)-1].GetTimestamp()),
	).Return(nil).Times(1)
	s.mockTaskGeneratorForNew.EXPECT().generateUserTimerTasks(
		s.stateBuilder.unixNanoToTime(newRunEvents[len(newRunEvents)-1].GetTimestamp()),
	).Return(nil).Times(1)

	newRunStateBuilder, err := s.stateBuilder.applyEvents(
		testNamespaceID, requestID, execution, s.toHistory(continueAsNewEvent), newRunEvents, false,
	)
	s.Nil(err)
	s.NotNil(newRunStateBuilder)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeWorkflowExecutionContinuedAsNew_EmptyNewRunHistory() {
	version := int64(1)
	requestID := uuid.New()
	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	newRunID := uuid.New()

	continueAsNewEvent := &historypb.HistoryEvent{
		Version:   version,
		EventId:   130,
		Timestamp: now.UnixNano(),
		EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_CONTINUED_AS_NEW,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionContinuedAsNewEventAttributes{WorkflowExecutionContinuedAsNewEventAttributes: &historypb.WorkflowExecutionContinuedAsNewEventAttributes{
			NewExecutionRunId: newRunID,
		}},
	}

	s.mockMutableState.EXPECT().ReplicateWorkflowExecutionContinuedAsNewEvent(
		continueAsNewEvent.GetEventId(),
		testNamespaceID,
		continueAsNewEvent,
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().GetNamespaceEntry().Return(testGlobalNamespaceEntry).AnyTimes()
	s.mockUpdateVersion(continueAsNewEvent)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockTaskGenerator.EXPECT().generateWorkflowCloseTasks(
		s.stateBuilder.unixNanoToTime(continueAsNewEvent.GetTimestamp()),
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	// new workflow namespace
	s.mockNamespaceCache.EXPECT().GetNamespace(testParentNamespace).Return(testGlobalParentNamespaceEntry, nil).AnyTimes()
	newRunStateBuilder, err := s.stateBuilder.applyEvents(
		testNamespaceID, requestID, execution, s.toHistory(continueAsNewEvent), nil, false,
	)
	s.Nil(err)
	s.Nil(newRunStateBuilder)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeWorkflowExecutionSignaled() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_SIGNALED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionSignaledEventAttributes{WorkflowExecutionSignaledEventAttributes: &historypb.WorkflowExecutionSignaledEventAttributes{}},
	}
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockMutableState.EXPECT().ReplicateWorkflowExecutionSignaled(event).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeWorkflowExecutionCancelRequested() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}
	now := time.Now()
	evenType := enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_CANCEL_REQUESTED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionCancelRequestedEventAttributes{WorkflowExecutionCancelRequestedEventAttributes: &historypb.WorkflowExecutionCancelRequestedEventAttributes{}},
	}

	s.mockMutableState.EXPECT().ReplicateWorkflowExecutionCancelRequestedEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeUpsertWorkflowSearchAttributes() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_UPSERT_WORKFLOW_SEARCH_ATTRIBUTES
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_UpsertWorkflowSearchAttributesEventAttributes{UpsertWorkflowSearchAttributesEventAttributes: &historypb.UpsertWorkflowSearchAttributesEventAttributes{}},
	}
	s.mockMutableState.EXPECT().ReplicateUpsertWorkflowSearchAttributesEvent(event).Return().Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockTaskGenerator.EXPECT().generateWorkflowSearchAttrTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeMarkerRecorded() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_MARKER_RECORDED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_MarkerRecordedEventAttributes{MarkerRecordedEventAttributes: &historypb.MarkerRecordedEventAttributes{}},
	}
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

// decision operations

func (s *stateBuilderSuite) TestApplyEvents_EventTypeDecisionTaskScheduled() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	tasklist := "some random tasklist"
	timeoutSecond := int32(11)
	evenType := enumspb.EVENT_TYPE_DECISION_TASK_SCHEDULED
	decisionAttempt := int64(111)
	event := &historypb.HistoryEvent{
		Version:   version,
		EventId:   130,
		Timestamp: now.UnixNano(),
		EventType: evenType,
		Attributes: &historypb.HistoryEvent_DecisionTaskScheduledEventAttributes{DecisionTaskScheduledEventAttributes: &historypb.DecisionTaskScheduledEventAttributes{
			TaskList:                   &tasklistpb.TaskList{Name: tasklist},
			StartToCloseTimeoutSeconds: timeoutSecond,
			Attempt:                    decisionAttempt,
		}},
	}
	di := &decisionInfo{
		Version:         event.GetVersion(),
		ScheduleID:      event.GetEventId(),
		StartedID:       common.EmptyEventID,
		RequestID:       emptyUUID,
		DecisionTimeout: timeoutSecond,
		TaskList:        tasklist,
		Attempt:         decisionAttempt,
	}
	executionInfo := &persistence.WorkflowExecutionInfo{
		TaskList: tasklist,
	}
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(executionInfo).AnyTimes()
	s.mockMutableState.EXPECT().ReplicateDecisionTaskScheduledEvent(
		event.GetVersion(), event.GetEventId(), tasklist, timeoutSecond, decisionAttempt, event.GetTimestamp(), event.GetTimestamp(),
	).Return(di, nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockTaskGenerator.EXPECT().generateDecisionScheduleTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
		di.ScheduleID,
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}
func (s *stateBuilderSuite) TestApplyEvents_EventTypeDecisionTaskStarted() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	tasklist := "some random tasklist"
	timeoutSecond := int32(11)
	scheduleID := int64(111)
	decisionRequestID := uuid.New()
	evenType := enumspb.EVENT_TYPE_DECISION_TASK_STARTED
	event := &historypb.HistoryEvent{
		Version:   version,
		EventId:   130,
		Timestamp: now.UnixNano(),
		EventType: evenType,
		Attributes: &historypb.HistoryEvent_DecisionTaskStartedEventAttributes{DecisionTaskStartedEventAttributes: &historypb.DecisionTaskStartedEventAttributes{
			ScheduledEventId: scheduleID,
			RequestId:        decisionRequestID,
		}},
	}
	di := &decisionInfo{
		Version:         event.GetVersion(),
		ScheduleID:      scheduleID,
		StartedID:       event.GetEventId(),
		RequestID:       decisionRequestID,
		DecisionTimeout: timeoutSecond,
		TaskList:        tasklist,
		Attempt:         0,
	}
	s.mockMutableState.EXPECT().ReplicateDecisionTaskStartedEvent(
		(*decisionInfo)(nil), event.GetVersion(), scheduleID, event.GetEventId(), decisionRequestID, event.GetTimestamp(),
	).Return(di, nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockTaskGenerator.EXPECT().generateDecisionStartTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
		di.ScheduleID,
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeDecisionTaskTimedOut() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	scheduleID := int64(12)
	startedID := int64(28)
	evenType := enumspb.EVENT_TYPE_DECISION_TASK_TIMED_OUT
	event := &historypb.HistoryEvent{
		Version:   version,
		EventId:   130,
		Timestamp: now.UnixNano(),
		EventType: evenType,
		Attributes: &historypb.HistoryEvent_DecisionTaskTimedOutEventAttributes{DecisionTaskTimedOutEventAttributes: &historypb.DecisionTaskTimedOutEventAttributes{
			ScheduledEventId: scheduleID,
			StartedEventId:   startedID,
			TimeoutType:      enumspb.TIMEOUT_TYPE_START_TO_CLOSE,
		}},
	}
	s.mockMutableState.EXPECT().ReplicateDecisionTaskTimedOutEvent(enumspb.TIMEOUT_TYPE_START_TO_CLOSE).Return(nil).Times(1)
	tasklist := "some random tasklist"
	newScheduleID := int64(233)
	executionInfo := &persistence.WorkflowExecutionInfo{
		TaskList: tasklist,
	}
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(executionInfo).AnyTimes()
	s.mockMutableState.EXPECT().ReplicateTransientDecisionTaskScheduled().Return(&decisionInfo{
		Version:    version,
		ScheduleID: newScheduleID,
		TaskList:   tasklist,
	}, nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockTaskGenerator.EXPECT().generateDecisionScheduleTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
		newScheduleID,
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeDecisionTaskFailed() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	scheduleID := int64(12)
	startedID := int64(28)
	evenType := enumspb.EVENT_TYPE_DECISION_TASK_FAILED
	event := &historypb.HistoryEvent{
		Version:   version,
		EventId:   130,
		Timestamp: now.UnixNano(),
		EventType: evenType,
		Attributes: &historypb.HistoryEvent_DecisionTaskFailedEventAttributes{DecisionTaskFailedEventAttributes: &historypb.DecisionTaskFailedEventAttributes{
			ScheduledEventId: scheduleID,
			StartedEventId:   startedID,
		}},
	}
	s.mockMutableState.EXPECT().ReplicateDecisionTaskFailedEvent().Return(nil).Times(1)
	tasklist := "some random tasklist"
	newScheduleID := int64(233)
	executionInfo := &persistence.WorkflowExecutionInfo{
		TaskList: tasklist,
	}
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(executionInfo).AnyTimes()
	s.mockMutableState.EXPECT().ReplicateTransientDecisionTaskScheduled().Return(&decisionInfo{
		Version:    version,
		ScheduleID: newScheduleID,
		TaskList:   tasklist,
	}, nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockTaskGenerator.EXPECT().generateDecisionScheduleTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
		newScheduleID,
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeDecisionTaskCompleted() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	scheduleID := int64(12)
	startedID := int64(28)
	evenType := enumspb.EVENT_TYPE_DECISION_TASK_COMPLETED
	event := &historypb.HistoryEvent{
		Version:   version,
		EventId:   130,
		Timestamp: now.UnixNano(),
		EventType: evenType,
		Attributes: &historypb.HistoryEvent_DecisionTaskCompletedEventAttributes{DecisionTaskCompletedEventAttributes: &historypb.DecisionTaskCompletedEventAttributes{
			ScheduledEventId: scheduleID,
			StartedEventId:   startedID,
		}},
	}
	s.mockMutableState.EXPECT().ReplicateDecisionTaskCompletedEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

// user timer operations

func (s *stateBuilderSuite) TestApplyEvents_EventTypeTimerStarted() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	timerID := "timer ID"
	timeoutSecond := int64(10)
	evenType := enumspb.EVENT_TYPE_TIMER_STARTED
	event := &historypb.HistoryEvent{
		Version:   version,
		EventId:   130,
		Timestamp: now.UnixNano(),
		EventType: evenType,
		Attributes: &historypb.HistoryEvent_TimerStartedEventAttributes{TimerStartedEventAttributes: &historypb.TimerStartedEventAttributes{
			TimerId:                   timerID,
			StartToFireTimeoutSeconds: timeoutSecond,
		}},
	}
	expiryTime, _ := types.TimestampProto(time.Unix(0, event.GetTimestamp()).Add(time.Duration(timeoutSecond) * time.Second))
	ti := &persistenceblobs.TimerInfo{
		Version:    event.GetVersion(),
		TimerId:    timerID,
		ExpiryTime: expiryTime,
		StartedId:  event.GetEventId(),
		TaskStatus: timerTaskStatusNone,
	}
	s.mockMutableState.EXPECT().ReplicateTimerStartedEvent(event).Return(ti, nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	// assertion on timer generated is in `mockUpdateVersion` function, since activity / user timer
	// need to be refreshed each time
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeTimerFired() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_TIMER_FIRED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_TimerFiredEventAttributes{TimerFiredEventAttributes: &historypb.TimerFiredEventAttributes{}},
	}

	s.mockMutableState.EXPECT().ReplicateTimerFiredEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	// assertion on timer generated is in `mockUpdateVersion` function, since activity / user timer
	// need to be refreshed each time
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeCancelTimerFailed() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_CANCEL_TIMER_FAILED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_CancelTimerFailedEventAttributes{CancelTimerFailedEventAttributes: &historypb.CancelTimerFailedEventAttributes{}},
	}
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	// assertion on timer generated is in `mockUpdateVersion` function, since activity / user timer
	// need to be refreshed each time
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)
	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeTimerCanceled() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()

	evenType := enumspb.EVENT_TYPE_TIMER_CANCELED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_TimerCanceledEventAttributes{TimerCanceledEventAttributes: &historypb.TimerCanceledEventAttributes{}},
	}
	s.mockMutableState.EXPECT().ReplicateTimerCanceledEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	// assertion on timer generated is in `mockUpdateVersion` function, since activity / user timer
	// need to be refreshed each time
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

// activity operations

func (s *stateBuilderSuite) TestApplyEvents_EventTypeActivityTaskScheduled() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	activityID := "activity ID"
	tasklist := "some random tasklist"
	timeoutSecond := int32(10)
	evenType := enumspb.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_ActivityTaskScheduledEventAttributes{ActivityTaskScheduledEventAttributes: &historypb.ActivityTaskScheduledEventAttributes{}},
	}

	ai := &persistence.ActivityInfo{
		Version:                  event.GetVersion(),
		ScheduleID:               event.GetEventId(),
		ScheduledEventBatchID:    event.GetEventId(),
		ScheduledEvent:           event,
		ScheduledTime:            time.Unix(0, event.GetTimestamp()),
		StartedID:                common.EmptyEventID,
		StartedTime:              time.Time{},
		ActivityID:               activityID,
		ScheduleToStartTimeout:   timeoutSecond,
		ScheduleToCloseTimeout:   timeoutSecond,
		StartToCloseTimeout:      timeoutSecond,
		HeartbeatTimeout:         timeoutSecond,
		CancelRequested:          false,
		CancelRequestID:          common.EmptyEventID,
		LastHeartBeatUpdatedTime: time.Time{},
		TimerTaskStatus:          timerTaskStatusNone,
		TaskList:                 tasklist,
	}
	executionInfo := &persistence.WorkflowExecutionInfo{
		TaskList: tasklist,
	}
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(executionInfo).AnyTimes()
	s.mockMutableState.EXPECT().ReplicateActivityTaskScheduledEvent(event.GetEventId(), event).Return(ai, nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockTaskGenerator.EXPECT().generateActivityTransferTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
		event,
	).Return(nil).Times(1)
	// assertion on timer generated is in `mockUpdateVersion` function, since activity / user timer
	// need to be refreshed each time
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeActivityTaskStarted() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	tasklist := "some random tasklist"
	evenType := enumspb.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED
	scheduledEvent := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_ActivityTaskScheduledEventAttributes{ActivityTaskScheduledEventAttributes: &historypb.ActivityTaskScheduledEventAttributes{}},
	}

	evenType = enumspb.EVENT_TYPE_ACTIVITY_TASK_STARTED
	startedEvent := &historypb.HistoryEvent{
		Version:    version,
		EventId:    scheduledEvent.GetEventId() + 1,
		Timestamp:  scheduledEvent.GetTimestamp() + 1000,
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_ActivityTaskStartedEventAttributes{ActivityTaskStartedEventAttributes: &historypb.ActivityTaskStartedEventAttributes{}},
	}

	executionInfo := &persistence.WorkflowExecutionInfo{
		TaskList: tasklist,
	}
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(executionInfo).AnyTimes()
	s.mockMutableState.EXPECT().ReplicateActivityTaskStartedEvent(startedEvent).Return(nil).Times(1)
	s.mockUpdateVersion(startedEvent)
	// assertion on timer generated is in `mockUpdateVersion` function, since activity / user timer
	// need to be refreshed each time
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(startedEvent), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeActivityTaskTimedOut() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_ACTIVITY_TASK_TIMED_OUT
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_ActivityTaskTimedOutEventAttributes{ActivityTaskTimedOutEventAttributes: &historypb.ActivityTaskTimedOutEventAttributes{}},
	}

	s.mockMutableState.EXPECT().ReplicateActivityTaskTimedOutEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	// assertion on timer generated is in `mockUpdateVersion` function, since activity / user timer
	// need to be refreshed each time// assertion on timer generated is in `mockUpdateVersion` function, since activity / user timer
	//	// need to be refreshed each time
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeActivityTaskFailed() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_ACTIVITY_TASK_FAILED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_ActivityTaskFailedEventAttributes{ActivityTaskFailedEventAttributes: &historypb.ActivityTaskFailedEventAttributes{}},
	}

	s.mockMutableState.EXPECT().ReplicateActivityTaskFailedEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	// assertion on timer generated is in `mockUpdateVersion` function, since activity / user timer
	// need to be refreshed each time
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeActivityTaskCompleted() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_ACTIVITY_TASK_COMPLETED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_ActivityTaskCompletedEventAttributes{ActivityTaskCompletedEventAttributes: &historypb.ActivityTaskCompletedEventAttributes{}},
	}

	s.mockMutableState.EXPECT().ReplicateActivityTaskCompletedEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	// assertion on timer generated is in `mockUpdateVersion` function, since activity / user timer
	// need to be refreshed each time
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeActivityTaskCancelRequested() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_ACTIVITY_TASK_CANCEL_REQUESTED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_ActivityTaskCancelRequestedEventAttributes{ActivityTaskCancelRequestedEventAttributes: &historypb.ActivityTaskCancelRequestedEventAttributes{}},
	}
	s.mockMutableState.EXPECT().ReplicateActivityTaskCancelRequestedEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeActivityTaskCanceled() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_ACTIVITY_TASK_CANCELED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_ActivityTaskCanceledEventAttributes{ActivityTaskCanceledEventAttributes: &historypb.ActivityTaskCanceledEventAttributes{}},
	}

	s.mockMutableState.EXPECT().ReplicateActivityTaskCanceledEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	// assertion on timer generated is in `mockUpdateVersion` function, since activity / user timer
	// need to be refreshed each time
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

// child workflow operations

func (s *stateBuilderSuite) TestApplyEvents_EventTypeStartChildWorkflowExecutionInitiated() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}
	targetWorkflowID := "some random target workflow ID"

	now := time.Now()
	createRequestID := uuid.New()
	evenType := enumspb.EVENT_TYPE_START_CHILD_WORKFLOW_EXECUTION_INITIATED
	event := &historypb.HistoryEvent{
		Version:   version,
		EventId:   130,
		Timestamp: now.UnixNano(),
		EventType: evenType,
		Attributes: &historypb.HistoryEvent_StartChildWorkflowExecutionInitiatedEventAttributes{StartChildWorkflowExecutionInitiatedEventAttributes: &historypb.StartChildWorkflowExecutionInitiatedEventAttributes{
			Namespace:  testTargetNamespace,
			WorkflowId: targetWorkflowID,
		}},
	}

	ci := &persistence.ChildExecutionInfo{
		Version:               event.GetVersion(),
		InitiatedID:           event.GetEventId(),
		InitiatedEventBatchID: event.GetEventId(),
		StartedID:             common.EmptyEventID,
		CreateRequestID:       createRequestID,
		Namespace:             testTargetNamespace,
	}

	// the create request ID is generated inside, cannot assert equal
	s.mockMutableState.EXPECT().ReplicateStartChildWorkflowExecutionInitiatedEvent(
		event.GetEventId(), event, gomock.Any(),
	).Return(ci, nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockTaskGenerator.EXPECT().generateChildWorkflowTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
		event,
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeStartChildWorkflowExecutionFailed() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_START_CHILD_WORKFLOW_EXECUTION_FAILED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_StartChildWorkflowExecutionFailedEventAttributes{StartChildWorkflowExecutionFailedEventAttributes: &historypb.StartChildWorkflowExecutionFailedEventAttributes{}},
	}
	s.mockMutableState.EXPECT().ReplicateStartChildWorkflowExecutionFailedEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeChildWorkflowExecutionStarted() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_STARTED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_ChildWorkflowExecutionStartedEventAttributes{ChildWorkflowExecutionStartedEventAttributes: &historypb.ChildWorkflowExecutionStartedEventAttributes{}},
	}
	s.mockMutableState.EXPECT().ReplicateChildWorkflowExecutionStartedEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeChildWorkflowExecutionTimedOut() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_TIMED_OUT
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_ChildWorkflowExecutionTimedOutEventAttributes{ChildWorkflowExecutionTimedOutEventAttributes: &historypb.ChildWorkflowExecutionTimedOutEventAttributes{}},
	}
	s.mockMutableState.EXPECT().ReplicateChildWorkflowExecutionTimedOutEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeChildWorkflowExecutionTerminated() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_TERMINATED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_ChildWorkflowExecutionTerminatedEventAttributes{ChildWorkflowExecutionTerminatedEventAttributes: &historypb.ChildWorkflowExecutionTerminatedEventAttributes{}},
	}
	s.mockMutableState.EXPECT().ReplicateChildWorkflowExecutionTerminatedEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeChildWorkflowExecutionFailed() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_FAILED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_ChildWorkflowExecutionFailedEventAttributes{ChildWorkflowExecutionFailedEventAttributes: &historypb.ChildWorkflowExecutionFailedEventAttributes{}},
	}
	s.mockMutableState.EXPECT().ReplicateChildWorkflowExecutionFailedEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeChildWorkflowExecutionCompleted() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_COMPLETED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_ChildWorkflowExecutionCompletedEventAttributes{ChildWorkflowExecutionCompletedEventAttributes: &historypb.ChildWorkflowExecutionCompletedEventAttributes{}},
	}
	s.mockMutableState.EXPECT().ReplicateChildWorkflowExecutionCompletedEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

// cancel external workflow operations

func (s *stateBuilderSuite) TestApplyEvents_EventTypeRequestCancelExternalWorkflowExecutionInitiated() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	targetWorkflowID := "some random target workflow ID"
	targetRunID := uuid.New()
	childWorkflowOnly := true

	now := time.Now()
	cancellationRequestID := uuid.New()
	control := "some random control"
	evenType := enumspb.EVENT_TYPE_REQUEST_CANCEL_EXTERNAL_WORKFLOW_EXECUTION_INITIATED
	event := &historypb.HistoryEvent{
		Version:   version,
		EventId:   130,
		Timestamp: now.UnixNano(),
		EventType: evenType,
		Attributes: &historypb.HistoryEvent_RequestCancelExternalWorkflowExecutionInitiatedEventAttributes{RequestCancelExternalWorkflowExecutionInitiatedEventAttributes: &historypb.RequestCancelExternalWorkflowExecutionInitiatedEventAttributes{
			Namespace: testTargetNamespace,
			WorkflowExecution: &commonpb.WorkflowExecution{
				WorkflowId: targetWorkflowID,
				RunId:      targetRunID,
			},
			ChildWorkflowOnly: childWorkflowOnly,
			Control:           control,
		}},
	}
	rci := &persistenceblobs.RequestCancelInfo{
		Version:         event.GetVersion(),
		InitiatedId:     event.GetEventId(),
		CancelRequestId: cancellationRequestID,
	}

	// the cancellation request ID is generated inside, cannot assert equal
	s.mockMutableState.EXPECT().ReplicateRequestCancelExternalWorkflowExecutionInitiatedEvent(
		event.GetEventId(), event, gomock.Any(),
	).Return(rci, nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockTaskGenerator.EXPECT().generateRequestCancelExternalTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
		event,
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeRequestCancelExternalWorkflowExecutionFailed() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_REQUEST_CANCEL_EXTERNAL_WORKFLOW_EXECUTION_FAILED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_RequestCancelExternalWorkflowExecutionFailedEventAttributes{RequestCancelExternalWorkflowExecutionFailedEventAttributes: &historypb.RequestCancelExternalWorkflowExecutionFailedEventAttributes{}},
	}
	s.mockMutableState.EXPECT().ReplicateRequestCancelExternalWorkflowExecutionFailedEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeExternalWorkflowExecutionCancelRequested() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_EXTERNAL_WORKFLOW_EXECUTION_CANCEL_REQUESTED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_ExternalWorkflowExecutionCancelRequestedEventAttributes{ExternalWorkflowExecutionCancelRequestedEventAttributes: &historypb.ExternalWorkflowExecutionCancelRequestedEventAttributes{}},
	}
	s.mockMutableState.EXPECT().ReplicateExternalWorkflowExecutionCancelRequested(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeChildWorkflowExecutionCanceled() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_CANCELED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_ChildWorkflowExecutionCanceledEventAttributes{ChildWorkflowExecutionCanceledEventAttributes: &historypb.ChildWorkflowExecutionCanceledEventAttributes{}},
	}
	s.mockMutableState.EXPECT().ReplicateChildWorkflowExecutionCanceledEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

// signal external workflow operations

func (s *stateBuilderSuite) TestApplyEvents_EventTypeSignalExternalWorkflowExecutionInitiated() {
	version := int64(1)
	requestID := uuid.New()
	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}
	targetWorkflowID := "some random target workflow ID"
	targetRunID := uuid.New()
	childWorkflowOnly := true

	now := time.Now()
	signalRequestID := uuid.New()
	signalName := "some random signal name"
	signalInput := payloads.EncodeString("some random signal input")
	control := "some random control"
	evenType := enumspb.EVENT_TYPE_SIGNAL_EXTERNAL_WORKFLOW_EXECUTION_INITIATED
	event := &historypb.HistoryEvent{
		Version:   version,
		EventId:   130,
		Timestamp: now.UnixNano(),
		EventType: evenType,
		Attributes: &historypb.HistoryEvent_SignalExternalWorkflowExecutionInitiatedEventAttributes{SignalExternalWorkflowExecutionInitiatedEventAttributes: &historypb.SignalExternalWorkflowExecutionInitiatedEventAttributes{
			Namespace: testTargetNamespace,
			WorkflowExecution: &commonpb.WorkflowExecution{
				WorkflowId: targetWorkflowID,
				RunId:      targetRunID,
			},
			SignalName:        signalName,
			Input:             signalInput,
			ChildWorkflowOnly: childWorkflowOnly,
		}},
	}
	si := &persistenceblobs.SignalInfo{
		Version:     event.GetVersion(),
		InitiatedId: event.GetEventId(),
		RequestId:   signalRequestID,
		Name:        signalName,
		Input:       signalInput,
		Control:     control,
	}

	// the cancellation request ID is generated inside, cannot assert equal
	s.mockMutableState.EXPECT().ReplicateSignalExternalWorkflowExecutionInitiatedEvent(
		event.GetEventId(), event, gomock.Any(),
	).Return(si, nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockTaskGenerator.EXPECT().generateSignalExternalTasks(
		s.stateBuilder.unixNanoToTime(event.GetTimestamp()),
		event,
	).Return(nil).Times(1)
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeSignalExternalWorkflowExecutionFailed() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_SIGNAL_EXTERNAL_WORKFLOW_EXECUTION_FAILED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_SignalExternalWorkflowExecutionFailedEventAttributes{SignalExternalWorkflowExecutionFailedEventAttributes: &historypb.SignalExternalWorkflowExecutionFailedEventAttributes{}},
	}
	s.mockMutableState.EXPECT().ReplicateSignalExternalWorkflowExecutionFailedEvent(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEvents_EventTypeExternalWorkflowExecutionSignaled() {
	version := int64(1)
	requestID := uuid.New()

	execution := commonpb.WorkflowExecution{
		WorkflowId: "some random workflow ID",
		RunId:      testRunID,
	}

	now := time.Now()
	evenType := enumspb.EVENT_TYPE_EXTERNAL_WORKFLOW_EXECUTION_SIGNALED
	event := &historypb.HistoryEvent{
		Version:    version,
		EventId:    130,
		Timestamp:  now.UnixNano(),
		EventType:  evenType,
		Attributes: &historypb.HistoryEvent_ExternalWorkflowExecutionSignaledEventAttributes{ExternalWorkflowExecutionSignaledEventAttributes: &historypb.ExternalWorkflowExecutionSignaledEventAttributes{}},
	}
	s.mockMutableState.EXPECT().ReplicateExternalWorkflowExecutionSignaled(event).Return(nil).Times(1)
	s.mockUpdateVersion(event)
	s.mockMutableState.EXPECT().GetExecutionInfo().Return(&persistence.WorkflowExecutionInfo{}).AnyTimes()
	s.mockMutableState.EXPECT().ClearStickyness().Times(1)

	_, err := s.stateBuilder.applyEvents(testNamespaceID, requestID, execution, s.toHistory(event), nil, false)
	s.Nil(err)
}

func (s *stateBuilderSuite) TestApplyEventsNewEventsNotHandled() {
	eventTypes := enumspb.EventType_value
	s.Equal(43, len(eventTypes), "If you see this error, you are adding new event type. "+
		"Before updating the number to make this test pass, please make sure you update stateBuilderImpl.applyEvents method "+
		"to handle the new decision type. Otherwise cross dc will not work on the new event.")
}
