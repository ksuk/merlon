package server

import (
	"reflect"
	"slices"

	"github.com/ksuk/merlon/api/internal/domain"
)

// customerIdentityChangedFields records only which persisted fields changed.
// Identity history is audit metadata, not a second copy of customer identity
// values. Keeping values solely in customers.attributes avoids exposing direct
// PII through the history endpoint while still telling an operator what was
// changed.
func customerIdentityChangedFields(before, after *domain.Customer) map[string]any {
	changed := map[string]any{}
	if after == nil {
		return changed
	}

	beforeAttributes := map[string]any{}
	if before != nil {
		beforeAttributes = before.Attributes
	}
	for field, value := range after.Attributes {
		if previous, ok := beforeAttributes[field]; before == nil || !ok || !reflect.DeepEqual(previous, value) {
			changed[field] = true
		}
	}
	if before != nil {
		for field := range beforeAttributes {
			if _, ok := after.Attributes[field]; !ok {
				changed[field] = true
			}
		}
	}

	if before == nil || before.CountryCode != after.CountryCode {
		changed["country_code"] = true
	}
	if before == nil || before.EffectiveStatus() != after.EffectiveStatus() {
		changed["status"] = true
	}
	if (before == nil && len(after.ProductTypes) > 0) || (before != nil && !slices.Equal(before.ProductTypes, after.ProductTypes)) {
		changed["product_types"] = true
	}
	return changed
}
