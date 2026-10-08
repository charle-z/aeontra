package modelturn

import (
	"errors"
	"strings"
	"unicode"
)

const (
	TaskStateActive   = "active"
	TaskStateBlocked  = "blocked"
	TaskStateComplete = "complete"
)

var (
	ErrCompletionStateRequired = errors.New("model turn task_state is required")
	ErrActiveWithoutToolCalls  = errors.New("model turn task_state active requires tool_calls")
	ErrCompleteWithoutStop     = errors.New("model turn task_state complete requires stop")
	ErrBlockedWithoutFailure   = errors.New("model turn task_state blocked requires error or cancelled")
	ErrTruncatedResponse       = errors.New("truncated model response cannot complete the turn")
	ErrPrematureCompletion     = errors.New("model turn completion rejected: response declares pending work; use task_state active with an offered tool call or blocked with an explicit failure")
)

// ValidateCompletionState enforces the only response shapes that advance a
// managed runtime. Active responses must continue through an offered tool,
// blocked responses must fail explicitly, and complete responses must not
// announce an action that has not run yet.
func ValidateCompletionState(taskState, finishReason, text string, toolCallCount int) error {
	if finishReason == "length" {
		return ErrTruncatedResponse
	}
	switch taskState {
	case TaskStateActive:
		if finishReason != "tool_calls" || toolCallCount == 0 {
			return ErrActiveWithoutToolCalls
		}
	case TaskStateComplete:
		if finishReason != "stop" || toolCallCount != 0 {
			return ErrCompleteWithoutStop
		}
		if DeclaresPendingAction(text) {
			return ErrPrematureCompletion
		}
	case TaskStateBlocked:
		if (finishReason != "error" && finishReason != "cancelled") || toolCallCount != 0 {
			return ErrBlockedWithoutFailure
		}
	default:
		return ErrCompletionStateRequired
	}
	return nil
}

// CompletionStateForFinishReason preserves compatibility for durable response
// records created before task_state was added to the public MCP contract.
func CompletionStateForFinishReason(finishReason string) string {
	switch finishReason {
	case "tool_calls":
		return TaskStateActive
	case "stop":
		return TaskStateComplete
	case "error", "cancelled":
		return TaskStateBlocked
	default:
		return ""
	}
}

var pendingActionPrefixes = []string{
	"now i will ",
	"now i'll ",
	"now i am going to ",
	"now i'm going to ",
	"i will now ",
	"i'll now ",
	"i am now going to ",
	"i'm now going to ",
	"i will continue",
	"i'll continue",
	"i am going to continue",
	"i'm going to continue",
	"the next step is ",
	"the next step will be ",
	"next i will ",
	"next i'll ",
	"next i am going to ",
	"next i'm going to ",
	"i will proceed to ",
	"i'll proceed to ",
	"it remains to ",
	"i still need to ",
	"we still need to ",
	"ahora voy a ",
	"ahora procederé a ",
	"ahora continuaré",
	"voy a continuar",
	"continuaré con ",
	"el siguiente paso es ",
	"el próximo paso es ",
	"procederé a ",
	"a continuación probaré",
	"a continuación voy a ",
	"queda por comprobar",
	"queda por verificar",
	"todavía falta ",
	"aún falta ",
}

// DeclaresPendingAction recognizes conservative, sentence-leading statements
// of future work. Markdown code and quotations are excluded so a final report
// may describe or test the gate without being mistaken for an active plan.
func DeclaresPendingAction(text string) bool {
	text = strings.ReplaceAll(text, "’", "'")
	text = strings.ReplaceAll(text, "‘", "'")
	for _, clause := range pendingActionClauses(text) {
		clause = strings.ToLower(strings.TrimSpace(clause))
		clause = strings.TrimLeftFunc(clause, func(r rune) bool {
			return unicode.IsSpace(r) || r == '-' || r == '*' || r == '•'
		})
		for _, replacement := range [][2]string{{"now, ", "now "}, {"next, ", "next "}, {"ahora, ", "ahora "}, {"a continuación, ", "a continuación "}} {
			if strings.HasPrefix(clause, replacement[0]) {
				clause = replacement[1] + strings.TrimPrefix(clause, replacement[0])
				break
			}
		}
		for _, prefix := range pendingActionPrefixes {
			if strings.HasPrefix(clause, prefix) {
				return true
			}
		}
	}
	return false
}

func pendingActionClauses(text string) []string {
	lines := strings.Split(text, "\n")
	visible := make([]string, 0, len(lines))
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || strings.HasPrefix(trimmed, ">") {
			continue
		}
		visible = append(visible, removeInlineCode(line))
	}
	return strings.FieldsFunc(strings.Join(visible, "\n"), func(r rune) bool {
		switch r {
		case '.', '!', '?', ';', '\n', '\r':
			return true
		default:
			return false
		}
	})
}

func removeInlineCode(text string) string {
	var builder strings.Builder
	inCode := false
	for _, r := range text {
		if r == '`' {
			inCode = !inCode
			continue
		}
		if !inCode {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}
