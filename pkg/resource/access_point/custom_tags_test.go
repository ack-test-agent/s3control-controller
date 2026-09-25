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
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	ackv1alpha1 "github.com/aws-controllers-k8s/runtime/apis/core/v1alpha1"
	ackmetrics "github.com/aws-controllers-k8s/runtime/pkg/metrics"
	"github.com/aws/aws-sdk-go-v2/aws"
	svcsdk "github.com/aws/aws-sdk-go-v2/service/s3control"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svcapitypes "github.com/aws-controllers-k8s/s3control-controller/apis/v1alpha1"
)

const testAPARN = "arn:aws:s3:us-west-2:012345678901:accesspoint/ap-tagged"

// tagsHTTPClient is a mock aws.HTTPClient for the s3control tagging
// operations. All three share the path /v20180820/tags/{ResourceArn+} and are
// distinguished by HTTP method:
//
//	ListTagsForResource  GET
//	TagResource          POST
//	UntagResource        DELETE  (tag keys in the ?tagKeys= query param)
//
// GetAccessPoint (GET /v20180820/accesspoint/{Name}) and GetAccessPointPolicy
// (.../policy) are also served so sdkFind can run end to end.
type tagsHTTPClient struct {
	requests []*http.Request
	bodies   []string

	listTagsBody string
}

func (c *tagsHTTPClient) Do(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		body = string(b)
	}
	c.requests = append(c.requests, req)
	c.bodies = append(c.bodies, body)

	respBody := ""
	switch {
	case strings.Contains(req.URL.Path, "/tags/") && req.Method == http.MethodGet:
		respBody = c.listTagsBody
	case strings.HasSuffix(req.URL.Path, "/policy"):
		respBody = `<?xml version="1.0" encoding="UTF-8"?>
<GetAccessPointPolicyResult><Policy>{"Version":"2012-10-17"}</Policy></GetAccessPointPolicyResult>`
	case strings.Contains(req.URL.Path, "/accesspoint/"):
		respBody = `<?xml version="1.0" encoding="UTF-8"?>
<GetAccessPointResult>
  <Name>ap-tagged</Name>
  <Bucket>test-bucket</Bucket>
  <AccessPointArn>` + testAPARN + `</AccessPointArn>
</GetAccessPointResult>`
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/xml"}},
		Body:       io.NopCloser(strings.NewReader(respBody)),
	}, nil
}

// find returns the first recorded request matching method against the tagging
// path, together with its body.
func (c *tagsHTTPClient) findTagOp(method string) (*http.Request, string) {
	for i, r := range c.requests {
		if r.Method == method && strings.Contains(r.URL.Path, "/tags/") {
			return c.requests[i], c.bodies[i]
		}
	}
	return nil, ""
}

func newTagsResourceManager(httpc *tagsHTTPClient) *resourceManager {
	return &resourceManager{
		sdkapi: svcsdk.New(svcsdk.Options{
			HTTPClient: httpc,
			Region:     "us-west-2",
		}),
		metrics: ackmetrics.NewMetrics("s3control"),
	}
}

func tag(k, v string) *svcapitypes.Tag {
	return &svcapitypes.Tag{Key: aws.String(k), Value: aws.String(v)}
}

func resourceWithTags(tags []*svcapitypes.Tag, withARN bool) *resource {
	ko := &svcapitypes.AccessPoint{}
	ko.Spec.AccountID = aws.String(testAccountID)
	ko.Spec.Name = aws.String("ap-tagged")
	ko.Spec.Tags = tags
	if withARN {
		arn := ackv1alpha1.AWSResourceName(testAPARN)
		ko.Status.ACKResourceMetadata = &ackv1alpha1.ResourceMetadata{ARN: &arn}
	}
	return &resource{ko: ko}
}

// TestSdkFind_TagsReadFromListTagsForResource is the regression guard for the
// permanent-delta/update-loop failure mode. GetAccessPoint does NOT return
// tags, so without the supplementary ListTagsForResource read, latest.Spec.Tags
// would always be nil against a non-nil desired.
func TestSdkFind_TagsReadFromListTagsForResource(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	httpc := &tagsHTTPClient{
		listTagsBody: `<?xml version="1.0" encoding="UTF-8"?>
<ListTagsForResourceResult>
  <Tags>
    <Tag><Key>env</Key><Value>prod</Value></Tag>
    <Tag><Key>team</Key><Value>ack</Value></Tag>
  </Tags>
</ListTagsForResourceResult>`,
	}
	rm := newTagsResourceManager(httpc)

	latest, err := rm.sdkFind(context.Background(), resourceWithTags(nil, false))
	require.NoError(err)
	require.NotNil(latest)

	listReq, _ := httpc.findTagOp(http.MethodGet)
	require.NotNil(listReq, "sdkFind must issue ListTagsForResource; GetAccessPoint does not return tags")
	assert.Equal(testAccountID, listReq.Header.Get("X-Amz-Account-Id"))

	require.Len(latest.ko.Spec.Tags, 2)
	got := map[string]string{}
	for _, tg := range latest.ko.Spec.Tags {
		got[*tg.Key] = *tg.Value
	}
	assert.Equal(map[string]string{"env": "prod", "team": "ack"}, got)
}

// TestSdkFind_TagReadErrorTolerated asserts a failing ListTagsForResource does
// not abort the whole read -- otherwise the resource could never reach Synced.
func TestSdkFind_TagReadErrorTolerated(t *testing.T) {
	require := require.New(t)

	httpc := &errorTagsHTTPClient{}
	rm := &resourceManager{
		sdkapi:  svcsdk.New(svcsdk.Options{HTTPClient: httpc, Region: "us-west-2"}),
		metrics: ackmetrics.NewMetrics("s3control"),
	}

	latest, err := rm.sdkFind(context.Background(), resourceWithTags(nil, false))
	require.NoError(err, "a NoSuchAccessPoint from ListTagsForResource must be tolerated")
	require.NotNil(latest)
}

// errorTagsHTTPClient answers the tagging GET with a NoSuchAccessPoint error
// and delegates everything else to tagsHTTPClient.
type errorTagsHTTPClient struct {
	tagsHTTPClient
}

func (c *errorTagsHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, "/tags/") && req.Method == http.MethodGet {
		c.requests = append(c.requests, req)
		c.bodies = append(c.bodies, "")
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     http.StatusText(http.StatusNotFound),
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body: io.NopCloser(strings.NewReader(
				`<?xml version="1.0" encoding="UTF-8"?><ErrorResponse><Error><Code>NoSuchAccessPoint</Code><Message>not found</Message></Error></ErrorResponse>`)),
		}, nil
	}
	return c.tagsHTTPClient.Do(req)
}

// TestSyncTags_AddsUpdatesAndRemoves asserts syncTags translates the tag diff
// into exactly one UntagResource call for removed keys and one TagResource call
// for added/changed keys.
func TestSyncTags_AddsUpdatesAndRemoves(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	httpc := &tagsHTTPClient{}
	rm := newTagsResourceManager(httpc)

	desired := resourceWithTags([]*svcapitypes.Tag{
		tag("env", "staging"), // changed
		tag("owner", "ack"),   // added
	}, true)
	latest := resourceWithTags([]*svcapitypes.Tag{
		tag("env", "prod"),   // changed
		tag("stale", "true"), // removed
	}, true)

	require.NoError(rm.syncTags(context.Background(), desired, latest))

	untagReq, _ := httpc.findTagOp(http.MethodDelete)
	require.NotNil(untagReq, "removed tag keys must be sent to UntagResource")
	q, err := url.ParseQuery(untagReq.URL.RawQuery)
	require.NoError(err)
	assert.Equal([]string{"stale"}, q["tagKeys"])
	assert.Equal(testAccountID, untagReq.Header.Get("X-Amz-Account-Id"))

	tagReq, tagBody := httpc.findTagOp(http.MethodPost)
	require.NotNil(tagReq, "added/changed tags must be sent to TagResource")
	assert.Contains(tagBody, "<Key>env</Key>")
	assert.Contains(tagBody, "<Value>staging</Value>")
	assert.Contains(tagBody, "<Key>owner</Key>")
	assert.NotContains(tagBody, "stale")
}

// TestSyncTags_NoopWhenNoDiff asserts an identical tag set issues no API calls,
// and that AWS-managed "aws:" tags are never added or removed.
func TestSyncTags_NoopWhenNoDiff(t *testing.T) {
	require := require.New(t)

	httpc := &tagsHTTPClient{}
	rm := newTagsResourceManager(httpc)

	desired := resourceWithTags([]*svcapitypes.Tag{tag("env", "prod")}, true)
	latest := resourceWithTags([]*svcapitypes.Tag{
		tag("env", "prod"),
		tag("aws:cloudformation:stack-name", "my-stack"),
	}, true)

	require.NoError(rm.syncTags(context.Background(), desired, latest))
	require.Empty(httpc.requests, "no tag API call expected when only aws: tags differ")
}

// TestSyncTags_NoARNIsNoop asserts syncTags bails out rather than panicking
// when the resource has no ARN yet (e.g. S3 on Outposts, where
// CreateAccessPoint does not return one).
func TestSyncTags_NoARNIsNoop(t *testing.T) {
	require := require.New(t)

	httpc := &tagsHTTPClient{}
	rm := newTagsResourceManager(httpc)

	desired := resourceWithTags([]*svcapitypes.Tag{tag("env", "prod")}, false)
	latest := resourceWithTags(nil, false)

	require.NoError(rm.syncTags(context.Background(), desired, latest))
	require.Empty(httpc.requests)
}
