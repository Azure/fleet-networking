/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package eventrecorder

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
)

func TestNoteTruncatingRecorderEventf(t *testing.T) {
	overLimit := strings.Repeat("a", NoteLengthLimit+10)
	// The 3-byte rune starts right before the cut-off so that a naive byte cut would split it.
	multiByteAtCutOff := strings.Repeat("a", NoteLengthLimit-len(truncatedNoteSuffix)-1) + "€" + strings.Repeat("b", 10)

	tests := []struct {
		name     string
		note     string
		args     []any
		wantNote string
	}{
		{
			name:     "short note with args",
			note:     "Failed to update %s: %v",
			args:     []any{"profile", errors.New("boom")},
			wantNote: "Failed to update profile: boom",
		},
		{
			name:     "note without args",
			note:     "Deleted Azure Traffic Manager endpoints",
			wantNote: "Deleted Azure Traffic Manager endpoints",
		},
		{
			name:     "note at the limit",
			note:     "%s",
			args:     []any{strings.Repeat("a", NoteLengthLimit)},
			wantNote: strings.Repeat("a", NoteLengthLimit),
		},
		{
			name:     "note over the limit",
			note:     "error: %v",
			args:     []any{overLimit},
			wantNote: ("error: " + overLimit)[:NoteLengthLimit-len(truncatedNoteSuffix)] + truncatedNoteSuffix,
		},
		{
			name:     "note over the limit with a multi-byte character at the cut-off",
			note:     "%s",
			args:     []any{multiByteAtCutOff},
			wantNote: strings.Repeat("a", NoteLengthLimit-len(truncatedNoteSuffix)-1) + truncatedNoteSuffix,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeRecorder := events.NewFakeRecorder(1)
			recorder := NewNoteTruncatingRecorder(fakeRecorder)

			recorder.Eventf(&corev1.Pod{}, nil, corev1.EventTypeWarning, "Reason", "Action", tc.note, tc.args...)

			got := <-fakeRecorder.Events
			wantPrefix := corev1.EventTypeWarning + " Reason "
			if !strings.HasPrefix(got, wantPrefix) {
				t.Fatalf("Eventf() recorded %q, want prefix %q", got, wantPrefix)
			}
			gotNote := strings.TrimPrefix(got, wantPrefix)
			if gotNote != tc.wantNote {
				t.Errorf("Eventf() recorded note %q, want %q", gotNote, tc.wantNote)
			}
			if len(gotNote) > NoteLengthLimit || !utf8.ValidString(gotNote) {
				t.Errorf("Eventf() recorded note of %d bytes (valid UTF-8: %t), want at most %d bytes of valid UTF-8", len(gotNote), utf8.ValidString(gotNote), NoteLengthLimit)
			}
		})
	}
}
