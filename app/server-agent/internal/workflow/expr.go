package workflow

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// Eval returns true if the 'when' expression matches the given context.
//
// Supported expressions:
//
//	""                                      → always true (default)
//	"default"                               → always true
//	"success"                               → status == NodeStatusSuccess
//	"failure"                               → status == NodeStatusFailure
//	"artifacts.KEY == 'VALUE'"
//	"artifacts.KEY != ''"
//	"errorCount >= N"                       (also >, <, <=, ==, !=)
//	"status == 'success'"
//	"event.kind == 'android.activity.created'"
func Eval(when string, status NodeStatus, artifacts map[string]string, errorCount int, eventKind domain.EventKind) bool {
	when = strings.TrimSpace(when)
	switch when {
	case "", "default":
		return true
	case "success":
		return status == NodeStatusSuccess
	case "failure":
		return status == NodeStatusFailure
	}

	// Three-token expression: LHS OP RHS
	lhs, op, rhs, ok := parseExpr(when)
	if !ok {
		return false
	}

	switch {
	case lhs == "status":
		lhsVal := string(status)
		rhsVal := stripQuotes(rhs)
		return compareStrings(lhsVal, op, rhsVal)

	case strings.HasPrefix(lhs, "artifacts."):
		key := strings.TrimPrefix(lhs, "artifacts.")
		lhsVal := artifacts[key]
		rhsVal := stripQuotes(rhs)
		return compareStrings(lhsVal, op, rhsVal)

	case lhs == "errorCount":
		rhsN, err := strconv.Atoi(strings.TrimSpace(rhs))
		if err != nil {
			return false
		}
		return compareInts(errorCount, op, rhsN)

	case lhs == "event.kind":
		return compareStrings(string(eventKind), op, stripQuotes(rhs))
	}

	slog.Default().Warn("workflow expr: unrecognized LHS in when expression",
		"expr", when, "lhs", lhs, "op", op)
	return false
}

// parseExpr splits "LHS OP RHS" into its three parts.
// OP is one of ==, !=, >=, <=, >, <.
func parseExpr(expr string) (lhs, op, rhs string, ok bool) {
	for _, candidate := range []string{"==", "!=", ">=", "<=", ">", "<"} {
		idx := strings.Index(expr, candidate)
		if idx < 0 {
			continue
		}
		// Make sure we pick the right operator when both ">" and ">=" match.
		// We iterate longest first (==, !=, >=, <= before >, <) so this is safe.
		l := strings.TrimSpace(expr[:idx])
		r := strings.TrimSpace(expr[idx+len(candidate):])
		return l, candidate, r, true
	}
	return "", "", "", false
}

func stripQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	return s
}

func compareStrings(lhs, op, rhs string) bool {
	switch op {
	case "==":
		return lhs == rhs
	case "!=":
		return lhs != rhs
	}
	return false
}

func compareInts(lhs int, op string, rhs int) bool {
	switch op {
	case "==":
		return lhs == rhs
	case "!=":
		return lhs != rhs
	case ">=":
		return lhs >= rhs
	case "<=":
		return lhs <= rhs
	case ">":
		return lhs > rhs
	case "<":
		return lhs < rhs
	}
	return false
}
