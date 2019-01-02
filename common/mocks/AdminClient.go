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

package mocks

import (
	"context"

	"github.com/stretchr/testify/mock"
	"github.com/uber/cadence/.gen/go/admin"
	"github.com/uber/cadence/.gen/go/admin/adminserviceclient"
	"github.com/uber/cadence/.gen/go/shared"
	"go.uber.org/yarpc"
)

// AdminClient is an autogenerated mock type for the Client type
type AdminClient struct {
	mock.Mock
}

var _ adminserviceclient.Interface = (*AdminClient)(nil)

// DescribeHistoryHost provides a mock function with given fields: ctx, request
func (_m *AdminClient) DescribeHistoryHost(ctx context.Context, request *shared.DescribeHistoryHostRequest, opts ...yarpc.CallOption) (*shared.DescribeHistoryHostResponse, error) {
	ret := _m.Called(ctx, request)

	var r0 *shared.DescribeHistoryHostResponse
	if rf, ok := ret.Get(0).(func(context.Context, *shared.DescribeHistoryHostRequest) *shared.DescribeHistoryHostResponse); ok {
		r0 = rf(ctx, request)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*shared.DescribeHistoryHostResponse)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *shared.DescribeHistoryHostRequest) error); ok {
		r1 = rf(ctx, request)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// DescribeWorkflowExecution provides a mock function with given fields: ctx, request
func (_m *AdminClient) DescribeWorkflowExecution(ctx context.Context, request *admin.DescribeWorkflowExecutionRequest, opts ...yarpc.CallOption) (*admin.DescribeWorkflowExecutionResponse, error) {
	ret := _m.Called(ctx, request)

	var r0 *admin.DescribeWorkflowExecutionResponse
	if rf, ok := ret.Get(0).(func(context.Context, *admin.DescribeWorkflowExecutionRequest) *admin.DescribeWorkflowExecutionResponse); ok {
		r0 = rf(ctx, request)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*admin.DescribeWorkflowExecutionResponse)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *admin.DescribeWorkflowExecutionRequest) error); ok {
		r1 = rf(ctx, request)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// GetWorkflowExecutionRawHistory provides a mock function with given fields: ctx, request
func (_m *AdminClient) GetWorkflowExecutionRawHistory(ctx context.Context, request *admin.GetWorkflowExecutionRawHistoryRequest, opts ...yarpc.CallOption) (*admin.GetWorkflowExecutionRawHistoryResponse, error) {
	ret := _m.Called(ctx, request)

	var r0 *admin.GetWorkflowExecutionRawHistoryResponse
	if rf, ok := ret.Get(0).(func(context.Context, *admin.GetWorkflowExecutionRawHistoryRequest) *admin.GetWorkflowExecutionRawHistoryResponse); ok {
		r0 = rf(ctx, request)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*admin.GetWorkflowExecutionRawHistoryResponse)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *admin.GetWorkflowExecutionRawHistoryRequest) error); ok {
		r1 = rf(ctx, request)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}