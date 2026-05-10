package service

import "testing"

func TestOpenAICompatibleEndpointBuildersAvoidDuplicateV1(t *testing.T) {
	cases := []struct {
		name       string
		base       string
		wantModels string
		wantImages string
		wantChat   string
	}{
		{
			name:       "root base",
			base:       "https://example.test",
			wantModels: "https://example.test/v1/models",
			wantImages: "https://example.test/v1/images/generations",
			wantChat:   "https://example.test/v1/chat/completions",
		},
		{
			name:       "v1 base",
			base:       "https://example.test/v1",
			wantModels: "https://example.test/v1/models",
			wantImages: "https://example.test/v1/images/generations",
			wantChat:   "https://example.test/v1/chat/completions",
		},
		{
			name:       "v1 base with trailing slash",
			base:       "https://example.test/v1/",
			wantModels: "https://example.test/v1/models",
			wantImages: "https://example.test/v1/images/generations",
			wantChat:   "https://example.test/v1/chat/completions",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := openAICompatibleModelsEndpoint(tc.base); got != tc.wantModels {
				t.Fatalf("models endpoint = %q, want %q", got, tc.wantModels)
			}
			if got := openAICompatibleImageEndpoint(tc.base); got != tc.wantImages {
				t.Fatalf("image endpoint = %q, want %q", got, tc.wantImages)
			}
			if got := openAICompatibleChatEndpoint(tc.base); got != tc.wantChat {
				t.Fatalf("chat endpoint = %q, want %q", got, tc.wantChat)
			}
		})
	}

	if got := openAICompatibleModelsEndpoint("https://example.test/v1/models"); got != "https://example.test/v1/models" {
		t.Fatalf("explicit models endpoint = %q", got)
	}
	if got := openAICompatibleImageEndpoint("https://example.test/v1/images/generations"); got != "https://example.test/v1/images/generations" {
		t.Fatalf("explicit image endpoint = %q", got)
	}
	if got := openAICompatibleChatEndpoint("https://example.test/v1/chat/completions"); got != "https://example.test/v1/chat/completions" {
		t.Fatalf("explicit chat endpoint = %q", got)
	}
}

func TestOpenAICompatibleAuthFailureDetection(t *testing.T) {
	if !openAICompatibleAuthFailure(`{"error":{"message":"Invalid API key"}}`) {
		t.Fatal("expected invalid API key response to be treated as auth failure")
	}
	if openAICompatibleAuthFailure(`{"error":{"message":"model is required"}}`) {
		t.Fatal("validation response should not be treated as auth failure")
	}
}
