package domain

import (
	"errors"
	"testing"
)

const (
	sha256Hola     = "b221d9dbb083a7f33428d7c2a3c3198ae925614d70210e28716ccaa7cd4ddb79"
	textoConSaltos = "primera pagina\nsegunda linea"
	sha256Saltos   = "2af80b293eab93adeab733a41a5822005a80960fb119cb043af3abc85dd2ad4d"
)

func TestNewExtractionResult_ChecksumMatchesSHA256(t *testing.T) {
	result := NewExtractionResult("hola", 12)
	if result.Checksum != sha256Hola {
		t.Errorf("Checksum = %q; want %q", result.Checksum, sha256Hola)
	}
}

func TestNewExtractionResult_PreservesRawNewlines(t *testing.T) {
	result := NewExtractionResult(textoConSaltos, 1)
	if result.Checksum != sha256Saltos {
		t.Errorf("Checksum = %q; want %q", result.Checksum, sha256Saltos)
	}
}

func TestNewExtractionResult_PreservesTextAndPageCount(t *testing.T) {
	result := NewExtractionResult(textoConSaltos, 12)
	if result.Text != textoConSaltos {
		t.Errorf("Text = %q; want %q", result.Text, textoConSaltos)
	}
	if result.PageCount != 12 {
		t.Errorf("PageCount = %d; want 12", result.PageCount)
	}
}

func TestExtractionResult_WithMethodsDoNotMutateOriginal(t *testing.T) {
	warnings := []Warning{{Code: "partial_extraction", Message: "page 3 skipped"}}
	original := NewExtractionResult("texto", 1)
	updated := original.WithExtractionTimeMs(42).WithWarnings(warnings)

	if original.ExtractionTimeMs != 0 || len(original.Warnings) != 0 {
		t.Fatal("With* methods mutated the original struct")
	}
	if updated.ExtractionTimeMs != 42 {
		t.Errorf("ExtractionTimeMs = %d; want 42", updated.ExtractionTimeMs)
	}
	if len(updated.Warnings) != 1 || updated.Warnings[0].Code != "partial_extraction" {
		t.Errorf("Warnings = %+v; want partial_extraction", updated.Warnings)
	}
}

func TestErrorTypes_AreDistinct(t *testing.T) {
	errs := []error{
		ErrEmptyDocument,
		ErrPasswordProtected,
		ErrUnsupportedFeatures,
		ErrExtractorFailure,
		ErrCorruptDocument,
		ErrEncryptedDocument,
	}
	for i := 0; i < len(errs); i++ {
		for j := i + 1; j < len(errs); j++ {
			if errors.Is(errs[i], errs[j]) {
				t.Errorf("errors %v and %v must be distinct", errs[i], errs[j])
			}
		}
	}
}
