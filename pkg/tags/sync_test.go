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
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	ackmetrics "github.com/aws-controllers-k8s/runtime/pkg/metrics"
	acktags "github.com/aws-controllers-k8s/runtime/pkg/tags"
	"github.com/aws/aws-sdk-go-v2/aws"
	svcsdk "github.com/aws/aws-sdk-go-v2/service/s3control"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAccountID   = "012345678901"
	testResourceARN = "arn:aws:s3:us-west-2:012345678901:accesspoint/ap-tagged"
)

// tagsHTTPClient is a mock aws.HTTPClient for the s3control tagging
// operations. TagResource (POST) and UntagResource (DELETE) share the path
// /v20180820/tags/{ResourceArn+} and are distinguished by HTTP method.
type tagsHTTPClient struct {
	requests []*http.Request
	bodies   []string
}

func (c *tagsHTTPClient) Do(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		body = string(b)
	}
	c.requests = append(c.requests, req)
	c.bodies = append(c.bodies, body)

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/xml"}},
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

// findTagOp returns the first recorded request matching method against the
// tagging path, together with its body.
func (c *tagsHTTPClient) findTagOp(method string) (*http.Request, string) {
	for i, r := range c.requests {
		if r.Method == method && strings.Contains(r.URL.Path, "/tags/") {
			return c.requests[i], c.bodies[i]
		}
	}
	return nil, ""
}

func newTestClient(httpc *tagsHTTPClient) (*svcsdk.Client, *ackmetrics.Metrics) {
	client := svcsdk.New(svcsdk.Options{
		HTTPClient: httpc,
		Region:     "us-west-2",
	})
	metrics := ackmetrics.NewMetrics("s3control")
	return client, metrics
}

// TestSyncTags_AddsUpdatesAndRemoves asserts SyncTags translates the tag diff
// into exactly one UntagResource call for removed keys and one TagResource call
// for added/changed keys.
func TestSyncTags_AddsUpdatesAndRemoves(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	httpc := &tagsHTTPClient{}
	client, metrics := newTestClient(httpc)

	accountID := aws.String(testAccountID)
	resourceARN := aws.String(testResourceARN)

	desiredTags := acktags.Tags{
		"env":   "staging", // changed
		"owner": "ack",     // added
	}
	latestTags := acktags.Tags{
		"env":   "prod", // changed
		"stale": "true", // removed
	}

	require.NoError(SyncTags(context.Background(), client, metrics, accountID, resourceARN, desiredTags, latestTags))

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

// TestSyncTags_NoopWhenNoDiff asserts an identical tag set issues no API calls.
// Both desired and latest contain the aws: tag (simulating the runtime's
// mirrorAWSTags behavior), so the diff is empty.
func TestSyncTags_NoopWhenNoDiff(t *testing.T) {
	require := require.New(t)

	httpc := &tagsHTTPClient{}
	client, metrics := newTestClient(httpc)

	accountID := aws.String(testAccountID)
	resourceARN := aws.String(testResourceARN)

	desiredTags := acktags.Tags{
		"env":                              "prod",
		"aws:cloudformation:stack-name":    "my-stack",
	}
	latestTags := acktags.Tags{
		"env":                              "prod",
		"aws:cloudformation:stack-name":    "my-stack",
	}

	require.NoError(SyncTags(context.Background(), client, metrics, accountID, resourceARN, desiredTags, latestTags))
	require.Empty(httpc.requests, "no tag API call expected when tags are identical")
}
