package main

import "strconv"
import "fmt"

const (
	// DefaultGRPCWindowSize is the default GRPC Window Size
	DefaultGRPCWindowSize = 1048576

	// DefaultIDBBatchSize to use if user has not provided in the config
	DefaultIDBBatchSize = 1024 * 100
	//DefaultIDBBatchFreq is 2 seconds
	DefaultIDBBatchFreq = 2000
	//DefaultIDBAccumulatorFreq is 2 seconds
	DefaultIDBAccumulatorFreq = 2000
	//DefaultIDBTimeout is 30 seconds
	DefaultIDBTimeout = 30

	// MatchExpressionXpath is for the pattern matching the xpath and key-value pairs
	MatchExpressionXpath = "\\/([^\\/]*)\\[(.*?)+?(?:\\])"
	// MatchExpressionKey is for pattern matching the single and multiple key value pairs
	MatchExpressionKey = "([A-Za-z0-9-/]*)=(.*?)?(?: and |$)+"
)

func convertToString(value interface{}) string {
	switch v := value.(type) {
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case bool:
		return strconv.FormatBool(v)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprintf("Unsupported type %T", v)
	}
}
