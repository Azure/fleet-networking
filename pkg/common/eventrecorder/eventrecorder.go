/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package eventrecorder features utilities for recording Kubernetes events with the events.k8s.io/v1 API.
package eventrecorder

import (
	"fmt"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
)

const (
	// NoteLengthLimit is the maximum length, in bytes, of the note of an events.k8s.io/v1 Event accepted by the
	// API server. Events with longer notes are rejected.
	NoteLengthLimit = 1024

	truncatedNoteSuffix = "..."
)

// noteTruncatingRecorder is an events.EventRecorder which truncates event notes exceeding NoteLengthLimit.
type noteTruncatingRecorder struct {
	recorder events.EventRecorder
}

var _ events.EventRecorder = &noteTruncatingRecorder{}

// NewNoteTruncatingRecorder returns an events.EventRecorder which truncates the formatted note of each event to at
// most NoteLengthLimit bytes before passing it to the given recorder, so that events carrying long messages (for
// example, Azure API errors) are not rejected by the API server.
func NewNoteTruncatingRecorder(recorder events.EventRecorder) events.EventRecorder {
	return &noteTruncatingRecorder{recorder: recorder}
}

// Eventf formats the note, truncates it if needed, and records the event with the underlying recorder.
func (r *noteTruncatingRecorder) Eventf(regarding runtime.Object, related runtime.Object, eventtype, reason, action, note string, args ...any) {
	r.recorder.Eventf(regarding, related, eventtype, reason, action, "%s", truncateNote(fmt.Sprintf(note, args...)))
}

// truncateNote truncates the note to at most NoteLengthLimit bytes without splitting a multi-byte character.
func truncateNote(note string) string {
	if len(note) <= NoteLengthLimit {
		return note
	}
	end := NoteLengthLimit - len(truncatedNoteSuffix)
	for end > 0 && !utf8.RuneStart(note[end]) {
		end--
	}
	return note[:end] + truncatedNoteSuffix
}
