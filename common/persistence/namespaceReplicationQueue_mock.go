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

// Code generated by MockGen. DO NOT EDIT.
// Source: namespaceReplicationQueue.go

// Package persistence is a generated GoMock package.
package persistence

import (
	gomock "github.com/golang/mock/gomock"
	repication "github.com/temporalio/temporal/.gen/proto/replication/v1"
	reflect "reflect"
)

// MockNamespaceReplicationQueue is a mock of NamespaceReplicationQueue interface
type MockNamespaceReplicationQueue struct {
	ctrl     *gomock.Controller
	recorder *MockNamespaceReplicationQueueMockRecorder
}

// MockNamespaceReplicationQueueMockRecorder is the mock recorder for MockNamespaceReplicationQueue
type MockNamespaceReplicationQueueMockRecorder struct {
	mock *MockNamespaceReplicationQueue
}

// NewMockNamespaceReplicationQueue creates a new mock instance
func NewMockNamespaceReplicationQueue(ctrl *gomock.Controller) *MockNamespaceReplicationQueue {
	mock := &MockNamespaceReplicationQueue{ctrl: ctrl}
	mock.recorder = &MockNamespaceReplicationQueueMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockNamespaceReplicationQueue) EXPECT() *MockNamespaceReplicationQueueMockRecorder {
	return m.recorder
}

// Start mocks base method
func (m *MockNamespaceReplicationQueue) Start() {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Start")
}

// Start indicates an expected call of Start
func (mr *MockNamespaceReplicationQueueMockRecorder) Start() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Start", reflect.TypeOf((*MockNamespaceReplicationQueue)(nil).Start))
}

// Stop mocks base method
func (m *MockNamespaceReplicationQueue) Stop() {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Stop")
}

// Stop indicates an expected call of Stop
func (mr *MockNamespaceReplicationQueueMockRecorder) Stop() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Stop", reflect.TypeOf((*MockNamespaceReplicationQueue)(nil).Stop))
}

// Publish mocks base method
func (m *MockNamespaceReplicationQueue) Publish(message interface{}) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Publish", message)
	ret0, _ := ret[0].(error)
	return ret0
}

// Publish indicates an expected call of Publish
func (mr *MockNamespaceReplicationQueueMockRecorder) Publish(message interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Publish", reflect.TypeOf((*MockNamespaceReplicationQueue)(nil).Publish), message)
}

// PublishToDLQ mocks base method
func (m *MockNamespaceReplicationQueue) PublishToDLQ(message interface{}) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PublishToDLQ", message)
	ret0, _ := ret[0].(error)
	return ret0
}

// PublishToDLQ indicates an expected call of PublishToDLQ
func (mr *MockNamespaceReplicationQueueMockRecorder) PublishToDLQ(message interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PublishToDLQ", reflect.TypeOf((*MockNamespaceReplicationQueue)(nil).PublishToDLQ), message)
}

// GetReplicationMessages mocks base method
func (m *MockNamespaceReplicationQueue) GetReplicationMessages(lastMessageID int64, maxCount int) ([]*repication.ReplicationTask, int64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetReplicationMessages", lastMessageID, maxCount)
	ret0, _ := ret[0].([]*repication.ReplicationTask)
	ret1, _ := ret[1].(int64)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// GetReplicationMessages indicates an expected call of GetReplicationMessages
func (mr *MockNamespaceReplicationQueueMockRecorder) GetReplicationMessages(lastMessageID, maxCount interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetReplicationMessages", reflect.TypeOf((*MockNamespaceReplicationQueue)(nil).GetReplicationMessages), lastMessageID, maxCount)
}

// UpdateAckLevel mocks base method
func (m *MockNamespaceReplicationQueue) UpdateAckLevel(lastProcessedMessageID int64, clusterName string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateAckLevel", lastProcessedMessageID, clusterName)
	ret0, _ := ret[0].(error)
	return ret0
}

// UpdateAckLevel indicates an expected call of UpdateAckLevel
func (mr *MockNamespaceReplicationQueueMockRecorder) UpdateAckLevel(lastProcessedMessageID, clusterName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateAckLevel", reflect.TypeOf((*MockNamespaceReplicationQueue)(nil).UpdateAckLevel), lastProcessedMessageID, clusterName)
}

// GetAckLevels mocks base method
func (m *MockNamespaceReplicationQueue) GetAckLevels() (map[string]int64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAckLevels")
	ret0, _ := ret[0].(map[string]int64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetAckLevels indicates an expected call of GetAckLevels
func (mr *MockNamespaceReplicationQueueMockRecorder) GetAckLevels() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAckLevels", reflect.TypeOf((*MockNamespaceReplicationQueue)(nil).GetAckLevels))
}

// GetMessagesFromDLQ mocks base method
func (m *MockNamespaceReplicationQueue) GetMessagesFromDLQ(firstMessageID, lastMessageID int64, pageSize int, pageToken []byte) ([]*repication.ReplicationTask, []byte, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetMessagesFromDLQ", firstMessageID, lastMessageID, pageSize, pageToken)
	ret0, _ := ret[0].([]*repication.ReplicationTask)
	ret1, _ := ret[1].([]byte)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// GetMessagesFromDLQ indicates an expected call of GetMessagesFromDLQ
func (mr *MockNamespaceReplicationQueueMockRecorder) GetMessagesFromDLQ(firstMessageID, lastMessageID, pageSize, pageToken interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetMessagesFromDLQ", reflect.TypeOf((*MockNamespaceReplicationQueue)(nil).GetMessagesFromDLQ), firstMessageID, lastMessageID, pageSize, pageToken)
}

// UpdateDLQAckLevel mocks base method
func (m *MockNamespaceReplicationQueue) UpdateDLQAckLevel(lastProcessedMessageID int64) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateDLQAckLevel", lastProcessedMessageID)
	ret0, _ := ret[0].(error)
	return ret0
}

// UpdateDLQAckLevel indicates an expected call of UpdateDLQAckLevel
func (mr *MockNamespaceReplicationQueueMockRecorder) UpdateDLQAckLevel(lastProcessedMessageID interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateDLQAckLevel", reflect.TypeOf((*MockNamespaceReplicationQueue)(nil).UpdateDLQAckLevel), lastProcessedMessageID)
}

// GetDLQAckLevel mocks base method
func (m *MockNamespaceReplicationQueue) GetDLQAckLevel() (int64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetDLQAckLevel")
	ret0, _ := ret[0].(int64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetDLQAckLevel indicates an expected call of GetDLQAckLevel
func (mr *MockNamespaceReplicationQueueMockRecorder) GetDLQAckLevel() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetDLQAckLevel", reflect.TypeOf((*MockNamespaceReplicationQueue)(nil).GetDLQAckLevel))
}

// RangeDeleteMessagesFromDLQ mocks base method
func (m *MockNamespaceReplicationQueue) RangeDeleteMessagesFromDLQ(firstMessageID, lastMessageID int64) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RangeDeleteMessagesFromDLQ", firstMessageID, lastMessageID)
	ret0, _ := ret[0].(error)
	return ret0
}

// RangeDeleteMessagesFromDLQ indicates an expected call of RangeDeleteMessagesFromDLQ
func (mr *MockNamespaceReplicationQueueMockRecorder) RangeDeleteMessagesFromDLQ(firstMessageID, lastMessageID interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RangeDeleteMessagesFromDLQ", reflect.TypeOf((*MockNamespaceReplicationQueue)(nil).RangeDeleteMessagesFromDLQ), firstMessageID, lastMessageID)
}

// DeleteMessageFromDLQ mocks base method
func (m *MockNamespaceReplicationQueue) DeleteMessageFromDLQ(messageID int64) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteMessageFromDLQ", messageID)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeleteMessageFromDLQ indicates an expected call of DeleteMessageFromDLQ
func (mr *MockNamespaceReplicationQueueMockRecorder) DeleteMessageFromDLQ(messageID interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteMessageFromDLQ", reflect.TypeOf((*MockNamespaceReplicationQueue)(nil).DeleteMessageFromDLQ), messageID)
}
