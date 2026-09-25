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
	"slices"

	ackrtlog "github.com/aws-controllers-k8s/runtime/pkg/runtime/log"
	"github.com/aws/aws-sdk-go-v2/aws"
	svcsdk "github.com/aws/aws-sdk-go-v2/service/s3control"
	svcsdktypes "github.com/aws/aws-sdk-go-v2/service/s3control/types"
)

// tolerableTagReadErrorCodes are the error codes that ListTagsForResource may
// return which must not fail the whole ReadOne. They all mean "there are no
// tags to be read right now", either because the access point has just gone
// away or because no tag set has ever been written.
var tolerableTagReadErrorCodes = []string{
	"NoSuchAccessPoint",
	"NoSuchTagSet",
	"NotFoundException",
	"ResourceNotFoundException",
}

// isTolerableTagReadError returns true if the supplied AWS error code from
// ListTagsForResource should be swallowed by the ReadOne tag-read hook.
func isTolerableTagReadError(code string) bool {
	return slices.Contains(tolerableTagReadErrorCodes, code)
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

	// AWS-managed tags ("aws:" prefix) can be neither written nor removed, so
	// exclude them from the diff on both sides.
	ignoreSystemTags(desiredTags, nil)
	ignoreSystemTags(latestTags, nil)

	var addedOrUpdatedKeys []string
	for k, v := range desiredTags {
		if lv, found := latestTags[k]; !found || lv != v {
			addedOrUpdatedKeys = append(addedOrUpdatedKeys, k)
		}
	}
	var removedKeys []string
	for k := range latestTags {
		if _, found := desiredTags[k]; !found {
			removedKeys = append(removedKeys, k)
		}
	}
	// Sorted purely for deterministic API calls.
	slices.Sort(addedOrUpdatedKeys)
	slices.Sort(removedKeys)

	// Remove first, so that a replace-all update cannot transiently exceed the
	// 50 tag per access point limit.
	if len(removedKeys) > 0 {
		rlog.Debug("removing tags from access point", "tag_keys", removedKeys)
		_, err = rm.sdkapi.UntagResource(ctx, &svcsdk.UntagResourceInput{
			AccountId:   accountID,
			ResourceArn: resourceARN,
			TagKeys:     removedKeys,
		})
		rm.metrics.RecordAPICall("UPDATE", "UntagResource", err)
		if err != nil {
			return err
		}
	}

	if len(addedOrUpdatedKeys) > 0 {
		rlog.Debug("adding tags to access point", "tag_keys", addedOrUpdatedKeys)
		tags := make([]svcsdktypes.Tag, 0, len(addedOrUpdatedKeys))
		for _, k := range addedOrUpdatedKeys {
			tags = append(tags, svcsdktypes.Tag{
				Key:   aws.String(k),
				Value: aws.String(desiredTags[k]),
			})
		}
		_, err = rm.sdkapi.TagResource(ctx, &svcsdk.TagResourceInput{
			AccountId:   accountID,
			ResourceArn: resourceARN,
			Tags:        tags,
		})
		rm.metrics.RecordAPICall("UPDATE", "TagResource", err)
		if err != nil {
			return err
		}
	}

	return nil
}
