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

package archiver

import (
	"context"
	"errors"

	"go.uber.org/cadence/activity"
)

type (
	// ArchiveOption is used to provide options for addding features to
	// the Archive method of History/Visibility Archiver
	ArchiveOption func(featureCatalog *ArchiveFeatureCatalog)

	// ArchiveFeatureCatalog is a collection features for the Archive method of
	// History/Visibility Archiver
	ArchiveFeatureCatalog struct {
		ProgressManager ProgressManager
	}

	// ProgressManager is used to record and load archive progress
	ProgressManager interface {
		RecordProgress(ctx context.Context, progress interface{}) error
		LoadProgress(ctx context.Context, valuePtr interface{}) error
	}
)

// GetFeatureCatalog applies all the ArchiveOptions to the catalog and returns the catalog.
// It should be called inside the Archive method.
func GetFeatureCatalog(opts ...ArchiveOption) *ArchiveFeatureCatalog {
	catalog := &ArchiveFeatureCatalog{}
	for _, opt := range opts {
		opt(catalog)
	}
	return catalog
}

// GetHeartbeatArchiveOption returns an ArchiveOption for enabling heartbeating.
// It should be used when the Archive method is invoked inside an activity.
func GetHeartbeatArchiveOption() ArchiveOption {
	return func(catalog *ArchiveFeatureCatalog) {
		catalog.ProgressManager = &heartbeatProgessManager{}
	}
}

type heartbeatProgessManager struct{}

func (h *heartbeatProgessManager) RecordProgress(ctx context.Context, progress interface{}) error {
	activity.RecordHeartbeat(ctx, progress)
	return nil
}

func (h *heartbeatProgessManager) LoadProgress(ctx context.Context, valuePtr interface{}) error {
	if !activity.HasHeartbeatDetails(ctx) {
		return errors.New("no progess information in the context")
	}
	return activity.GetHeartbeatDetails(ctx, valuePtr)
}
