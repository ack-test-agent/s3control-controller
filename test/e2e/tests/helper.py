# Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"). You may
# not use this file except in compliance with the License. A copy of the
# License is located at
#
#	 http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is distributed
# on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
# express or implied. See the License for the specific language governing
# permissions and limitations under the License.

"""Helper functions for S3Control e2e tests
"""

import logging

class S3ControlValidator:
    def __init__(self, s3control_client):
        self.s3control_client = s3control_client

    def get_access_point(self, account_id: str, name: str) -> dict:
        try:
            resp = self.s3control_client.get_access_point(
                AccountId=account_id,
                Name=name,
            )
            return resp

        except Exception as e:
            return None

    def access_point_exist(self, account_id: str, name: str) -> bool:
        return self.get_access_point(account_id, name) is not None

    def get_access_point_policy(self, account_id: str, name: str):
        try:
            resp = self.s3control_client.get_access_point_policy(
                AccountId=account_id,
                Name=name,
            )
            return resp.get('Policy')
        except Exception:
            return None

    def list_tags_for_resource(self, account_id: str, resource_arn: str):
        """Return the access point's tags as a list of {'Key':..,'Value':..}.

        GetAccessPoint does not return tags; they live behind the separate
        ListTagsForResource API, which is addressed by ARN rather than name.
        Returns None if the call fails (e.g. the access point is gone).
        """
        try:
            resp = self.s3control_client.list_tags_for_resource(
                AccountId=account_id,
                ResourceArn=resource_arn,
            )
            return resp.get('Tags', [])
        except Exception as e:
            logging.debug(f"list_tags_for_resource failed: {e}")
            return None

    def get_tags_dict(self, account_id: str, resource_arn: str):
        """Same as list_tags_for_resource but keyed by tag key for easy asserts."""
        tags = self.list_tags_for_resource(account_id, resource_arn)
        if tags is None:
            return None
        return {t['Key']: t['Value'] for t in tags}

