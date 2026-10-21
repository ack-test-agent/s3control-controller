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

package tags

import (
	"context"
	"slices"

	ackmetrics "github.com/aws-controllers-k8s/runtime/pkg/metrics"
	ackrtlog "github.com/aws-controllers-k8s/runtime/pkg/runtime/log"
	acktags "github.com/aws-controllers-k8s/runtime/pkg/tags"
	"github.com/aws/aws-sdk-go-v2/aws"
	svcsdk "github.com/aws/aws-sdk-go-v2/service/s3control"
	svcsdktypes "github.com/aws/aws-sdk-go-v2/service/s3control/types"
)

// TagClient is the subset of the S3 Control SDK client used by SyncTags.
// Accepting an interface instead of *svcsdk.Client makes the function
// testable without a full SDK client.
type TagClient interface {
	TagResource(ctx context.Context, params *svcsdk.TagResourceInput, optFns ...func(*svcsdk.Options)) (*svcsdk.TagResourceOutput, error)
	UntagResource(ctx context.Context, params *svcsdk.UntagResourceInput, optFns ...func(*svcsdk.Options)) (*svcsdk.UntagResourceOutput, error)
}

// TolerableTagReadErrorCodes are the error codes that ListTagsForResource may
// return which must not fail the whole ReadOne. They all mean "there are no
// tags to be read right now", either because the resource has just gone away
// or because no tag set has ever been written.
var TolerableTagReadErrorCodes = []string{
	"NoSuchAccessPoint",
	"NoSuchTagSet",
	"NotFoundException",
	"ResourceNotFoundException",
}

// IsTolerableTagReadError returns true if the supplied AWS error code from
// ListTagsForResource should be swallowed by the ReadOne tag-read hook.
func IsTolerableTagReadError(code string) bool {
	return slices.Contains(TolerableTagReadErrorCodes, code)
}

// SyncTags reconciles a resource's tags in AWS with the desired tags by
// computing the diff between desiredTags and latestTags and issuing
// UntagResource / TagResource calls as needed.
//
// S3 Control's TagResource and UntagResource are ARN-based and
// resource-agnostic, so this utility can be shared across all tagged
// resources in the controller (AccessPoint, Bucket, StorageLensGroup, etc.).
func SyncTags(
	ctx context.Context,
	sdkapi TagClient,
	metrics *ackmetrics.Metrics,
	accountID *string,
	resourceARN *string,
	desiredTags acktags.Tags,
	latestTags acktags.Tags,
) (err error) {
	rlog := ackrtlog.FromContext(ctx)
	exit := rlog.Trace("tags.SyncTags")
	defer func() { exit(err) }()

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
	// 50 tag per resource limit.
	if len(removedKeys) > 0 {
		rlog.Debug("removing tags from resource", "tag_keys", removedKeys)
		_, err = sdkapi.UntagResource(ctx, &svcsdk.UntagResourceInput{
			AccountId:   accountID,
			ResourceArn: resourceARN,
			TagKeys:     removedKeys,
		})
		metrics.RecordAPICall("UPDATE", "UntagResource", err)
		if err != nil {
			return err
		}
	}

	if len(addedOrUpdatedKeys) > 0 {
		rlog.Debug("adding tags to resource", "tag_keys", addedOrUpdatedKeys)
		tags := make([]svcsdktypes.Tag, 0, len(addedOrUpdatedKeys))
		for _, k := range addedOrUpdatedKeys {
			tags = append(tags, svcsdktypes.Tag{
				Key:   aws.String(k),
				Value: aws.String(desiredTags[k]),
			})
		}
		_, err = sdkapi.TagResource(ctx, &svcsdk.TagResourceInput{
			AccountId:   accountID,
			ResourceArn: resourceARN,
			Tags:        tags,
		})
		metrics.RecordAPICall("UPDATE", "TagResource", err)
		if err != nil {
			return err
		}
	}

	return nil
}
