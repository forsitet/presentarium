package errs_test

import (
	"errors"
	"net/http"
	"testing"

	"presentarium/internal/errs"
)

func TestAppError_Error(t *testing.T) {
	t.Run("with wrapped error", func(t *testing.T) {
		inner := errors.New("inner reason")
		ae := errs.New(http.StatusBadRequest, "bad input", inner)
		if ae.Error() != "bad input: inner reason" {
			t.Errorf("unexpected error string: %q", ae.Error())
		}
		if ae.Code != http.StatusBadRequest {
			t.Errorf("unexpected code: %d", ae.Code)
		}
	})

	t.Run("without wrapped error", func(t *testing.T) {
		ae := errs.New(http.StatusInternalServerError, "boom", nil)
		if ae.Error() != "boom" {
			t.Errorf("unexpected error string: %q", ae.Error())
		}
	})
}

func TestAppError_Unwrap(t *testing.T) {
	inner := errors.New("inner")
	ae := errs.New(400, "wrap", inner)
	if !errors.Is(ae, inner) {
		t.Error("expected errors.Is to find inner via Unwrap")
	}
	if ae.Unwrap() != inner {
		t.Errorf("Unwrap returned %v, want %v", ae.Unwrap(), inner)
	}
}

func TestSentinelHelpers(t *testing.T) {
	cases := []struct {
		name string
		err  error
		fn   func(error) bool
		want bool
	}{
		{"NotFound positive", errs.ErrNotFound, errs.IsNotFound, true},
		{"NotFound negative", errs.ErrConflict, errs.IsNotFound, false},
		{"Forbidden positive", errs.ErrForbidden, errs.IsForbidden, true},
		{"Forbidden negative", errs.ErrNotFound, errs.IsForbidden, false},
		{"Conflict positive", errs.ErrConflict, errs.IsConflict, true},
		{"Conflict negative", errs.ErrValidation, errs.IsConflict, false},
		{"Validation positive", errs.ErrValidation, errs.IsValidation, true},
		{"Validation negative", errs.ErrUnauthorized, errs.IsValidation, false},
		{"Unauthorized positive", errs.ErrUnauthorized, errs.IsUnauthorized, true},
		{"Unauthorized negative", errs.ErrNotFound, errs.IsUnauthorized, false},
		{"nil never matches", nil, errs.IsNotFound, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fn(tc.err); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAppError_WrapsSentinel(t *testing.T) {
	ae := errs.New(404, "missing", errs.ErrNotFound)
	if !errs.IsNotFound(ae) {
		t.Error("expected IsNotFound to detect wrapped sentinel via errors.Is")
	}
}
