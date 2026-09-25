# Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"). You may
# not use this file except in compliance with the License. A copy of the
# License is located at
#
# 	 http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is distributed
# on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
# express or implied. See the License for the specific language governing
# permissions and limitations under the License.

"""Integration tests for the S3 Control Access Point API.
"""

import json
import pytest
import time
import logging

from acktest.resources import random_suffix_name
from acktest.k8s import resource as k8s
from acktest.aws.identity import get_account_id, get_region

from e2e import service_marker, CRD_GROUP, CRD_VERSION, load_s3control_resource
from e2e.replacement_values import REPLACEMENT_VALUES
from e2e.bootstrap_resources import get_bootstrap_resources
from e2e.tests.helper import S3ControlValidator

RESOURCE_PLURAL = "accesspoints"

CREATE_WAIT_AFTER_SECONDS = 10
UPDATE_WAIT_AFTER_SECONDS = 10
DELETE_WAIT_AFTER_SECONDS = 10

# Tags set on create by the accesspoint_with_tags.yaml template.
TAG_KEY_1 = "environment"
TAG_VALUE_1 = "test"
TAG_KEY_2 = "purpose"
TAG_VALUE_2 = "ack-e2e"


def _cr_arn(ref) -> str:
    """Return the access point ARN recorded on the CR's status."""
    cr = k8s.get_resource(ref)
    assert cr is not None
    arn = cr["status"]["ackResourceMetadata"]["arn"]
    assert arn is not None
    return arn


def _cr_tags(ref) -> dict:
    """Return the CR's spec.tags as a {key: value} dict."""
    cr = k8s.get_resource(ref)
    assert cr is not None
    return {t["key"]: t["value"] for t in cr["spec"].get("tags") or []}

@pytest.fixture(scope="module")
def simple_access_point(s3control_client):

    resource_name = random_suffix_name("accesspoint", 24)

    account_id = get_account_id()
    replacements = REPLACEMENT_VALUES.copy()
    replacements["ACCESS_POINT_NAME"] = resource_name
    replacements["ACCOUNT_ID"] = account_id
    replacements["BUCKET_NAME"] = get_bootstrap_resources().Bucket.name

    resource_data = load_s3control_resource(
        "accesspoint",
        additional_replacements=replacements,
    )

    logging.debug(resource_data)

    # Create k8s resource
    ref = k8s.CustomResourceReference(
        CRD_GROUP, CRD_VERSION, RESOURCE_PLURAL,
        resource_name, namespace="default",
    )
    k8s.create_custom_resource(ref, resource_data)

    time.sleep(CREATE_WAIT_AFTER_SECONDS)
    cr = k8s.wait_resource_consumed_by_controller(ref)

    assert cr is not None
    assert k8s.get_resource_exists(ref)

    yield (ref, cr, resource_name)

    _, deleted = k8s.delete_custom_resource(
        ref,
        period_length=DELETE_WAIT_AFTER_SECONDS,
    )
    assert deleted

    time.sleep(DELETE_WAIT_AFTER_SECONDS)

    validator = S3ControlValidator(s3control_client)
    assert not validator.access_point_exist(account_id, resource_name)


def _make_policy(account_id: str, ap_name: str) -> str:
    """Return a simple S3 access point policy as a JSON string."""
    region = get_region()
    policy = {
        "Version": "2012-10-17",
        "Statement": [
            {
                "Effect": "Allow",
                "Principal": {"AWS": f"arn:aws:iam::{account_id}:root"},
                "Action": "s3:GetObject",
                "Resource": (
                    f"arn:aws:s3:{region}:{account_id}:accesspoint/{ap_name}/object/*"
                ),
            }
        ],
    }
    return json.dumps(policy)


@pytest.fixture(scope="module")
def access_point_with_policy(s3control_client):
    resource_name = random_suffix_name("ap-policy", 24)
    account_id = get_account_id()
    policy_doc = _make_policy(account_id, resource_name)

    replacements = REPLACEMENT_VALUES.copy()
    replacements["ACCESS_POINT_NAME"] = resource_name
    replacements["ACCOUNT_ID"] = account_id
    replacements["BUCKET_NAME"] = get_bootstrap_resources().Bucket.name
    replacements["POLICY_DOCUMENT"] = policy_doc

    resource_data = load_s3control_resource(
        "accesspoint_with_policy",
        additional_replacements=replacements,
    )

    ref = k8s.CustomResourceReference(
        CRD_GROUP, CRD_VERSION, RESOURCE_PLURAL,
        resource_name, namespace="default",
    )
    k8s.create_custom_resource(ref, resource_data)

    time.sleep(CREATE_WAIT_AFTER_SECONDS)
    cr = k8s.wait_resource_consumed_by_controller(ref)

    assert cr is not None
    assert k8s.get_resource_exists(ref)

    yield (ref, cr, resource_name)

    _, deleted = k8s.delete_custom_resource(
        ref,
        period_length=DELETE_WAIT_AFTER_SECONDS,
    )
    assert deleted

    time.sleep(DELETE_WAIT_AFTER_SECONDS)

    validator = S3ControlValidator(s3control_client)
    assert not validator.access_point_exist(account_id, resource_name)


@pytest.fixture(scope="module")
def access_point_no_policy(s3control_client):
    resource_name = random_suffix_name("ap-upd-pol", 24)
    account_id = get_account_id()

    replacements = REPLACEMENT_VALUES.copy()
    replacements["ACCESS_POINT_NAME"] = resource_name
    replacements["ACCOUNT_ID"] = account_id
    replacements["BUCKET_NAME"] = get_bootstrap_resources().Bucket.name

    resource_data = load_s3control_resource(
        "accesspoint",
        additional_replacements=replacements,
    )

    ref = k8s.CustomResourceReference(
        CRD_GROUP, CRD_VERSION, RESOURCE_PLURAL,
        resource_name, namespace="default",
    )
    k8s.create_custom_resource(ref, resource_data)

    time.sleep(CREATE_WAIT_AFTER_SECONDS)
    cr = k8s.wait_resource_consumed_by_controller(ref)

    assert cr is not None
    assert k8s.get_resource_exists(ref)

    yield (ref, cr, resource_name)

    _, deleted = k8s.delete_custom_resource(
        ref,
        period_length=DELETE_WAIT_AFTER_SECONDS,
    )
    assert deleted

    time.sleep(DELETE_WAIT_AFTER_SECONDS)

    validator = S3ControlValidator(s3control_client)
    assert not validator.access_point_exist(account_id, resource_name)


@pytest.fixture(scope="module")
def access_point_with_tags(s3control_client):
    resource_name = random_suffix_name("ap-tags", 24)
    account_id = get_account_id()

    replacements = REPLACEMENT_VALUES.copy()
    replacements["ACCESS_POINT_NAME"] = resource_name
    replacements["ACCOUNT_ID"] = account_id
    replacements["BUCKET_NAME"] = get_bootstrap_resources().Bucket.name
    replacements["TAG_KEY_1"] = TAG_KEY_1
    replacements["TAG_VALUE_1"] = TAG_VALUE_1
    replacements["TAG_KEY_2"] = TAG_KEY_2
    replacements["TAG_VALUE_2"] = TAG_VALUE_2

    resource_data = load_s3control_resource(
        "accesspoint_with_tags",
        additional_replacements=replacements,
    )

    logging.debug(resource_data)

    ref = k8s.CustomResourceReference(
        CRD_GROUP, CRD_VERSION, RESOURCE_PLURAL,
        resource_name, namespace="default",
    )
    k8s.create_custom_resource(ref, resource_data)

    time.sleep(CREATE_WAIT_AFTER_SECONDS)
    cr = k8s.wait_resource_consumed_by_controller(ref)

    assert cr is not None
    assert k8s.get_resource_exists(ref)

    yield (ref, cr, resource_name)

    _, deleted = k8s.delete_custom_resource(
        ref,
        period_length=DELETE_WAIT_AFTER_SECONDS,
    )
    assert deleted

    time.sleep(DELETE_WAIT_AFTER_SECONDS)

    validator = S3ControlValidator(s3control_client)
    assert not validator.access_point_exist(account_id, resource_name)


@service_marker
@pytest.mark.canary
class TestAccessPoint:
    def test_create_delete(self, s3control_client, simple_access_point):
        (ref, _, access_point_name) = simple_access_point
        assert access_point_name is not None
        account_id = get_account_id()

        validator = S3ControlValidator(s3control_client)
        assert validator.access_point_exist(account_id, access_point_name)

    def test_create_with_policy(self, s3control_client, access_point_with_policy):
        """Create an access point with a policy and verify it is set in AWS."""
        (ref, _, resource_name) = access_point_with_policy
        account_id = get_account_id()

        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=5)

        validator = S3ControlValidator(s3control_client)
        assert validator.access_point_exist(account_id, resource_name)
        aws_policy = validator.get_access_point_policy(account_id, resource_name)
        assert aws_policy is not None, "Expected policy to be set in AWS"

    def test_update_policy(self, s3control_client, access_point_no_policy):
        """Create access point without policy, then patch to add one."""
        (ref, _, resource_name) = access_point_no_policy
        account_id = get_account_id()

        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=5)

        # Verify no policy initially
        validator = S3ControlValidator(s3control_client)
        assert validator.get_access_point_policy(account_id, resource_name) is None

        # Patch with a policy
        policy_doc = _make_policy(account_id, resource_name)
        patch = {"spec": {"policy": policy_doc}}
        k8s.patch_custom_resource(ref, patch)
        time.sleep(UPDATE_WAIT_AFTER_SECONDS)

        aws_policy = validator.get_access_point_policy(account_id, resource_name)
        assert aws_policy is not None, "Expected policy to be set after update"

    def test_delete_policy(self, s3control_client, access_point_with_policy):
        """Create access point with policy, then remove it via patch."""
        (ref, _, resource_name) = access_point_with_policy
        account_id = get_account_id()

        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=5)

        validator = S3ControlValidator(s3control_client)
        assert validator.get_access_point_policy(account_id, resource_name) is not None

        # Remove policy by setting it to null
        patch = {"spec": {"policy": None}}
        k8s.patch_custom_resource(ref, patch)
        time.sleep(UPDATE_WAIT_AFTER_SECONDS)

        aws_policy = validator.get_access_point_policy(account_id, resource_name)
        assert aws_policy is None, "Expected policy to be removed after patch"

    def test_create_with_tags(self, s3control_client, access_point_with_tags):
        """Tags supplied on create must reach AWS and be readable back on the CR."""
        (ref, _, resource_name) = access_point_with_tags
        account_id = get_account_id()

        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=5)

        validator = S3ControlValidator(s3control_client)
        assert validator.access_point_exist(account_id, resource_name)

        # --- AWS-side verification
        aws_tags = validator.get_tags_dict(account_id, _cr_arn(ref))
        assert aws_tags is not None, "Expected ListTagsForResource to succeed"
        # Subset assertion only: ACK's MergeResourceTags injects its own
        # services.k8s.aws/* default tags, so exact equality would fail.
        assert aws_tags.get(TAG_KEY_1) == TAG_VALUE_1
        assert aws_tags.get(TAG_KEY_2) == TAG_VALUE_2

        # --- CR-side verification. This is the regression guard for the
        # update-loop failure mode: if the ReadOne tag hook did not populate
        # latest.Spec.Tags, the delta would never converge.
        cr_tags = _cr_tags(ref)
        assert cr_tags.get(TAG_KEY_1) == TAG_VALUE_1
        assert cr_tags.get(TAG_KEY_2) == TAG_VALUE_2

    def test_update_tags(self, s3control_client, access_point_with_tags):
        """Add, change and remove tags on an existing access point.

        Exercises both TagResource (add/change) and UntagResource (remove).
        """
        (ref, _, resource_name) = access_point_with_tags
        account_id = get_account_id()

        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=5)

        validator = S3ControlValidator(s3control_client)
        arn = _cr_arn(ref)

        new_key = "owner"
        new_value = "ack"
        updated_value = TAG_VALUE_1 + "-updated"

        # Change TAG_KEY_1's value, drop TAG_KEY_2, add a brand new key.
        patch = {
            "spec": {
                "tags": [
                    {"key": TAG_KEY_1, "value": updated_value},
                    {"key": new_key, "value": new_value},
                ]
            }
        }
        k8s.patch_custom_resource(ref, patch)
        time.sleep(UPDATE_WAIT_AFTER_SECONDS)

        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=10)

        # --- AWS-side verification
        aws_tags = validator.get_tags_dict(account_id, arn)
        assert aws_tags is not None
        assert aws_tags.get(TAG_KEY_1) == updated_value, "changed tag should be updated"
        assert aws_tags.get(new_key) == new_value, "added tag should be present"
        assert TAG_KEY_2 not in aws_tags, "removed tag key should be gone from AWS"

        # --- CR-side verification
        cr_tags = _cr_tags(ref)
        assert cr_tags.get(TAG_KEY_1) == updated_value
        assert cr_tags.get(new_key) == new_value
        assert TAG_KEY_2 not in cr_tags

        # --- No-drift check: the resource must settle rather than flap between
        # Synced=True/False on subsequent reconciles.
        time.sleep(UPDATE_WAIT_AFTER_SECONDS)
        assert k8s.wait_on_condition(ref, "ACK.ResourceSynced", "True", wait_periods=5)
        settled_tags = _cr_tags(ref)
        assert settled_tags.get(TAG_KEY_1) == updated_value
        assert settled_tags.get(new_key) == new_value
        assert TAG_KEY_2 not in settled_tags
