package service

import (
	"errors"
	"testing"
	"time"
)

func TestProviderCooldownGrokForbiddenIsTransient(t *testing.T) {
	err := errors.New(`grok upload HTTP 403: <!DOCTYPE html><html><head><title>Just a moment...</title></head></html>`)
	if got := providerCooldown(err); got != 0 {
		t.Fatalf("expected transient cooldown 0, got %s", got)
	}
}

func TestProviderCooldownRetryable429StillCooldowns(t *testing.T) {
	err := errors.New(`provider call: grok video HTTP 429: {"error":{"code":8,"message":"Too many requests"}}`)
	got := providerCooldown(err)
	if got < 30*time.Minute {
		t.Fatalf("expected 429 cooldown >= 30m, got %s", got)
	}
}

func TestRetryableProviderErrorCoversImageChannelFailover(t *testing.T) {
	cases := []error{
		errors.New(`provider call: gpt image2 images api 403: {"error":{"message":"Image generation is not enabled for this account"}}`),
		errors.New(`provider call: gpt image2 images api 502: <!DOCTYPE html><html class="no-js">`),
		errors.New(`provider call: gpt image2 nova 404: 404 page not found`),
		errors.New(`provider call: gpt image2 images api 400: {"error":{"message":"Unsupported size"}}`),
	}
	for _, err := range cases {
		if !retryableProviderError(err) {
			t.Fatalf("expected %q to be retryable", err.Error())
		}
	}
}

func TestRetryableProviderErrorDoesNotRetryBrokenReferenceImage(t *testing.T) {
	err := errors.New(`provider call: gpt image2 images edits reference 1: reference image download 404: not found`)
	if retryableProviderError(err) {
		t.Fatalf("reference image download failure should not be retried across providers")
	}
}
