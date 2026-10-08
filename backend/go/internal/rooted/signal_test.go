package rooted
package rooted

import (
	"errors"
	"testing"
)

func TestDurationBucketValid(t *testing.T) {
	valid := []DurationBucket{
		DurationLifelong,
		DurationManyYears,
		DurationSeveralYears,
		DurationAFewYears,
		DurationRecently,
		"lifelong",
		"many_years",
		"several_years",
		"a_few_years",
		"recently",
	}

	for _, bucket := range valid {
		if !bucket.Valid() {
			t.Errorf("Valid(%q) = false, want true", bucket)
		}
	}

	invalid := []DurationBucket{"", "years", "LIFELONG", "many years", "a_few_year", "unknown"}

	for _, bucket := range invalid {
		if bucket.Valid() {
			t.Errorf("Valid(%q) = true, want false", bucket)
		}
	}
}

func TestDurationBucketsListsTheFiveValues(t *testing.T) {
	want := []DurationBucket{
		"lifelong",
		"many_years",
		"several_years",
		"a_few_years",
		"recently",
	}

	if len(DurationBuckets) != len(want) {
		t.Fatalf("DurationBuckets has %d entries, want %d", len(DurationBuckets), len(want))
	}
	for i, bucket := range want {
		if DurationBuckets[i] != bucket {
			t.Errorf("DurationBuckets[%d] = %q, want %q", i, DurationBuckets[i], bucket)
		}
	}
}

func TestValidationErrorUnwrapsToSentinel(t *testing.T) {
	err := &ValidationError{Field: "place", Message: "is required"}

	if !errors.Is(err, ErrValidation) {
		t.Error("errors.Is(validation, ErrValidation) = false, want true")
	}

	var validation *ValidationError
	if !errors.As(error(err), &validation) {
		t.Error("errors.As(err, &validation) = false, want true")
	}
	if validation.Field != "place" {
		t.Errorf("field = %q, want %q", validation.Field, "place")
	}
	if err.Error() != "rooted: invalid place: is required" {
		t.Errorf("error = %q, want it to name the package, field, and message", err.Error())
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	if errors.Is(ErrValidation, ErrUserNotFound) {
		t.Error("ErrValidation and ErrUserNotFound are the same value, want distinct sentinels")
	}
}

func TestPlaceLimits(t *testing.T) {
	if MinPlaceLength != 1 {
		t.Errorf("MinPlaceLength = %d, want 1", MinPlaceLength)
	}
	if MaxPlaceLength != 80 {
		t.Errorf("MaxPlaceLength = %d, want 80", MaxPlaceLength)
	}
}
