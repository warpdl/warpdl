package warplib

import (
	"errors"
	"testing"
)

func TestValidateItemParts_NilPart(t *testing.T) {
	parts := map[int64]*ItemPart{0: {Hash: "p1", FinalOffset: 100}, 100: nil}
	err := ValidateItemParts(parts)
	if !errors.Is(err, ErrItemPartNil) {
		t.Fatalf("expected ErrItemPartNil, got %v", err)
	}
}

func TestValidateItemParts_InvalidRange(t *testing.T) {
	parts := map[int64]*ItemPart{100: {Hash: "p1", FinalOffset: 50}}
	err := ValidateItemParts(parts)
	if !errors.Is(err, ErrItemPartInvalidRange) {
		t.Fatalf("expected ErrItemPartInvalidRange, got %v", err)
	}
}

func TestValidateItemParts_ZeroRange(t *testing.T) {
	parts := map[int64]*ItemPart{100: {Hash: "p1", FinalOffset: 100}}
	err := ValidateItemParts(parts)
	if !errors.Is(err, ErrItemPartInvalidRange) {
		t.Fatalf("expected ErrItemPartInvalidRange for zero range, got %v", err)
	}
}

func TestValidateItemParts_Valid(t *testing.T) {
	parts := map[int64]*ItemPart{0: {Hash: "p1", FinalOffset: 99}, 100: {Hash: "p2", FinalOffset: 199}}
	if err := ValidateItemParts(parts); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateItemParts_Empty(t *testing.T) {
	parts := map[int64]*ItemPart{}
	if err := ValidateItemParts(parts); err != nil {
		t.Fatalf("expected nil for empty map, got %v", err)
	}
}

func TestValidateItemParts_Nil(t *testing.T) {
	var parts map[int64]*ItemPart
	if err := ValidateItemParts(parts); err != nil {
		t.Fatalf("expected nil for nil map, got %v", err)
	}
}
