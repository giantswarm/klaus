// Package a2a implements the A2A protocol surface for Klaus.
// It exposes Klaus's Prompter interface (both agent and chat modes) as an
// a2asrv.AgentExecutor so any A2A client can interact with the subprocess.
//
// This file is the only place in the package that directly touches the a2a-go
// SDK event/part types. Changes to the SDK (pre-1.0) should only require edits
// here.
package a2a

import (
	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
)

// workingEvent returns a TaskStateWorking status update with an optional
// status message. msg may be empty.
func workingEvent(reqCtx *a2asrv.RequestContext, msg string) *a2a.TaskStatusUpdateEvent {
	var message *a2a.Message
	if msg != "" {
		message = a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: msg})
	}
	return a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateWorking, message)
}

// artifactEvent returns a TaskArtifactUpdateEvent with LastChunk=true carrying
// the full response text. sessionID is attached as metadata when non-empty.
func artifactEvent(reqCtx *a2asrv.RequestContext, text, sessionID string) *a2a.TaskArtifactUpdateEvent {
	ev := a2a.NewArtifactEvent(reqCtx, a2a.TextPart{Text: text})
	ev.LastChunk = true
	if sessionID != "" {
		if ev.Artifact.Metadata == nil {
			ev.Artifact.Metadata = map[string]any{}
		}
		ev.Artifact.Metadata["session_id"] = sessionID
	}
	return ev
}

// completedEvent returns a terminal TaskStateCompleted status update with
// Final=true.
func completedEvent(reqCtx *a2asrv.RequestContext) *a2a.TaskStatusUpdateEvent {
	ev := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCompleted, nil)
	ev.Final = true
	return ev
}

// failedEvent returns a terminal TaskStateFailed status update with Final=true.
func failedEvent(reqCtx *a2asrv.RequestContext, errText string) *a2a.TaskStatusUpdateEvent {
	message := a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: errText})
	ev := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateFailed, message)
	ev.Final = true
	return ev
}

// rejectedEvent returns a terminal TaskStateRejected status update with
// Final=true. Used when the executor is busy with another context.
func rejectedEvent(reqCtx *a2asrv.RequestContext, reason string) *a2a.TaskStatusUpdateEvent {
	message := a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: reason})
	ev := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateRejected, message)
	ev.Final = true
	return ev
}

// canceledEvent returns a terminal TaskStateCanceled status update with
// Final=true.
func canceledEvent(reqCtx *a2asrv.RequestContext) *a2a.TaskStatusUpdateEvent {
	ev := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCanceled, nil)
	ev.Final = true
	return ev
}

// extractText pulls the first non-empty text part from an A2A message.
func extractText(msg *a2a.Message) string {
	if msg == nil {
		return ""
	}
	for _, part := range msg.Parts {
		if tp, ok := part.(a2a.TextPart); ok && tp.Text != "" {
			return tp.Text
		}
	}
	return ""
}
