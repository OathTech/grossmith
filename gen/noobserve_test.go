package gen

import (
	"errors"
	"testing"
)

// The E1 totality witness (audit P0: 13-16 of 256 NoObserve shape
// subsets panicked the generator at measured seeds, violating the
// package contract that bad configs are errors). Every subset over
// several seeds must either generate or return the typed error — never
// panic — and the closed-enum/duplicate checks reject at Validate.
func TestNoObserveIsTotal(t *testing.T) {
	for _, seed := range []int64{1, 42, 507} {
		all := []Shape{ShapeInt, ShapeBool, ShapeString, ShapeArray,
			ShapeStruct, ShapeMap, ShapeInterface, ShapeSlice}
		for mask := 0; mask < 256; mask++ {
			var shapes []Shape
			for b, sh := range all {
				if mask&(1<<b) != 0 {
					shapes = append(shapes, sh)
				}
			}
			cfg := DefaultConfig(seed)
			cfg.NoObserve = shapes
			_, err := New(cfg).Generate() // a panic here fails the test
			if err != nil && !errors.Is(err, ErrNothingObservable) {
				t.Fatalf("seed %d mask %08b: unexpected error class: %v", seed, mask, err)
			}
		}
	}
}

func TestNoObserveValidation(t *testing.T) {
	bad := DefaultConfig(1)
	bad.NoObserve = []Shape{Shape(255)}
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown shape accepted")
	}
	dup := DefaultConfig(1)
	dup.NoObserve = []Shape{ShapeSlice, ShapeSlice}
	if err := dup.Validate(); err == nil {
		t.Fatal("duplicate shape accepted")
	}
}
