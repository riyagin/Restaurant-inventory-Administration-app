package handler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func nilToEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case pgtype.Numeric:
		return int64(numericToFloat64(n))
	}
	return 0
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func joinStrings(ss []string, sep string) string {
	return strings.Join(ss, sep)
}

func mapGroupKey(m map[string]any, keys ...string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%v", m[k])
	}
	return strings.Join(parts, "::")
}
