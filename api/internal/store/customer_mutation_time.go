package store

import "time"

func nextCustomerMutationTime(previous, observed time.Time) time.Time {
	previous = previous.UTC().Truncate(time.Microsecond)
	observed = observed.UTC().Truncate(time.Microsecond)
	if !observed.After(previous) {
		return previous.Add(time.Microsecond)
	}
	return observed
}
