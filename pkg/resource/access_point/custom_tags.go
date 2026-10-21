// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package access_point

import (
	"context"

	ackrtlog "github.com/aws-controllers-k8s/runtime/pkg/runtime/log"

	"github.com/aws-controllers-k8s/s3control-controller/pkg/tags"
)

// isTolerableTagReadError returns true if the supplied AWS error code from
// ListTagsForResource should be swallowed by the ReadOne tag-read hook.
// This is a thin wrapper around the shared tags package so that the generated
// sdk.go (which calls package-local functions) does not need a cross-package
// import.
func isTolerableTagReadError(code string) bool {
	return tags.IsTolerableTagReadError(code)
}

// syncTags reconciles the access point's tags in AWS with the tags in the
// desired resource's Spec.
//
// S3 Control has no UpdateAccessPoint operation and tags are not part of the
// CreateAccessPoint/GetAccessPoint round trip beyond creation, so drift is
// resolved with the dedicated TagResource/UntagResource operations. This is
// invoked from customUpdate when the delta contains Spec.Tags.
func (rm *resourceManager) syncTags(
	ctx context.Context,
	desired *resource,
	latest *resource,
) (err error) {
	rlog := ackrtlog.FromContext(ctx)
	exit := rlog.Trace("rm.syncTags")
	defer func() {
		exit(err)
	}()

	// Both tagging operations are addressed by ARN. Without one there is
	// nothing we can do, and the next reconcile will have it.
	if latest.ko.Status.ACKResourceMetadata == nil ||
		latest.ko.Status.ACKResourceMetadata.ARN == nil {
		return nil
	}
	resourceARN := (*string)(latest.ko.Status.ACKResourceMetadata.ARN)

	accountID := desired.ko.Spec.AccountID
	if accountID == nil {
		accountID = latest.ko.Spec.AccountID
	}

	// Use the generated conversion helper rather than comparing []*Tag slices
	// positionally: TagList is an ordered list in the model but tags are
	// semantically a set, and positional comparison yields phantom diffs.
	desiredTags, _ := convertToOrderedACKTags(desired.ko.Spec.Tags)
	latestTags, _ := convertToOrderedACKTags(latest.ko.Spec.Tags)

	return tags.SyncTags(ctx, rm.sdkapi, rm.metrics, accountID, resourceARN, desiredTags, latestTags)
}
