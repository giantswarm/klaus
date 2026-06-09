// Package a2a implements the A2A protocol surface for Klaus.
// It exposes Klaus's Prompter interface (both agent and chat modes) as an
// a2asrv.AgentExecutor so any A2A client can interact with the subprocess.
//
// This file is the only place in the package that directly touches the a2a-go
// SDK event/part types. Changes to the SDK should only require edits here.
package a2a

import (
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/giantswarm/klaus/pkg/claude"
)

// workingEvent returns a TaskStateWorking status update with an optional
// status message. msg may be empty.
func workingEvent(execCtx *a2asrv.ExecutorContext, msg string) *a2a.TaskStatusUpdateEvent {
	var message *a2a.Message
	if msg != "" {
		message = a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(msg))
	}
	return a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, message)
}

// artifactEvent returns a TaskArtifactUpdateEvent with LastChunk=true carrying
// the full response text. sessionID and usage are attached as metadata when non-empty/nil.
// usage is stored under the generic "token_usage" key so callers can translate it to
// whatever downstream format they need without embedding that knowledge here.
func artifactEvent(execCtx *a2asrv.ExecutorContext, text, sessionID string, usage *claude.TokenUsage) *a2a.TaskArtifactUpdateEvent {
	ev := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(text))
	ev.LastChunk = true
	if sessionID != "" || usage != nil {
		if ev.Artifact.Metadata == nil {
			ev.Artifact.Metadata = map[string]any{}
		}
		if sessionID != "" {
			ev.Artifact.Metadata["session_id"] = sessionID
		}
		if usage != nil {
			ev.Artifact.Metadata["token_usage"] = map[string]any{
				"input_tokens":                usage.InputTokens,
				"output_tokens":               usage.OutputTokens,
				"cache_creation_input_tokens": usage.CacheCreationInputTokens,
				"cache_read_input_tokens":     usage.CacheReadInputTokens,
			}
		}
	}
	return ev
}

// completedEvent returns a terminal TaskStateCompleted status update.
func completedEvent(execCtx *a2asrv.ExecutorContext) *a2a.TaskStatusUpdateEvent {
	return a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil)
}

// failedEvent returns a terminal TaskStateFailed status update.
func failedEvent(execCtx *a2asrv.ExecutorContext, errText string) *a2a.TaskStatusUpdateEvent {
	message := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(errText))
	return a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, message)
}

// rejectedEvent returns a terminal TaskStateRejected status update.
// Used when the executor is busy with another context.
func rejectedEvent(execCtx *a2asrv.ExecutorContext, reason string) *a2a.TaskStatusUpdateEvent {
	message := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(reason))
	return a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateRejected, message)
}

// canceledEvent returns a terminal TaskStateCanceled status update.
func canceledEvent(execCtx *a2asrv.ExecutorContext) *a2a.TaskStatusUpdateEvent {
	return a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil)
}

// extractText concatenates all text parts from an A2A message.
func extractText(msg *a2a.Message) string {
	if msg == nil {
		return ""
	}
	var sb strings.Builder
	for _, part := range msg.Parts {
		if part != nil {
			sb.WriteString(part.Text())
		}
	}
	return sb.String()
}
