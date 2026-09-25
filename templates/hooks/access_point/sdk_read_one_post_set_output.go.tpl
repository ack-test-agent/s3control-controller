policyInput := &svcsdk.GetAccessPointPolicyInput{
	AccountId: r.ko.Spec.AccountID,
	Name:      r.ko.Spec.Name,
}
policyResp, policyErr := rm.sdkapi.GetAccessPointPolicy(ctx, policyInput)
rm.metrics.RecordAPICall("READ_ONE", "GetAccessPointPolicy", policyErr)
if policyErr != nil {
	var awsErr smithy.APIError
	if errors.As(policyErr, &awsErr) && awsErr.ErrorCode() == "NoSuchAccessPointPolicy" {
		ko.Spec.Policy = nil
	} else {
		return nil, policyErr
	}
} else {
	ko.Spec.Policy = policyResp.Policy
}

// GetAccessPoint does not return the access point's tags, so read them with a
// supplementary ListTagsForResource call. Without this, latest.Spec.Tags is
// always nil while desired.Spec.Tags is set, which makes newResourceDelta
// report a permanent Spec.Tags difference and the resource update-loops.
if ko.Status.ACKResourceMetadata != nil && ko.Status.ACKResourceMetadata.ARN != nil {
	tagsInput := &svcsdk.ListTagsForResourceInput{
		AccountId:   r.ko.Spec.AccountID,
		ResourceArn: (*string)(ko.Status.ACKResourceMetadata.ARN),
	}
	tagsResp, tagsErr := rm.sdkapi.ListTagsForResource(ctx, tagsInput)
	rm.metrics.RecordAPICall("READ_ONE", "ListTagsForResource", tagsErr)
	if tagsErr != nil {
		// Tolerate not-found style failures by leaving ko.Spec.Tags untouched
		// rather than aborting the whole read, which would prevent the
		// resource from ever reaching Synced.
		var awsErr smithy.APIError
		if !errors.As(tagsErr, &awsErr) || !isTolerableTagReadError(awsErr.ErrorCode()) {
			return nil, tagsErr
		}
	} else if len(tagsResp.Tags) > 0 {
		tags := make([]*svcapitypes.Tag, 0, len(tagsResp.Tags))
		for _, t := range tagsResp.Tags {
			tags = append(tags, &svcapitypes.Tag{
				Key:   t.Key,
				Value: t.Value,
			})
		}
		ko.Spec.Tags = tags
	} else {
		ko.Spec.Tags = nil
	}
}
